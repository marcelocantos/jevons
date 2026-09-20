// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package claudetrust pre-accepts Claude Code workspace trust for owner
// workdirs so a mint does not stall on the TUI "Quick safety check" dialog
// and then get retired as unbriefed_seat (🎯T709).
//
// ~/.claude.json is shared hot state (🎯T376): the edit is one project key,
// the document is carried as raw members, and a write is skipped when the
// flag is already true. Claude Code itself does not take the jevons lock,
// so a vendor flush between read and rename can still clobber — the same
// residual mcpscope declares.
package claudetrust

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/marcelocantos/claudia"
)

// ConfigEnv overrides the Claude Code config path. "off" disables writes.
const ConfigEnv = "JEVONS_CLAUDE_TRUST_CONFIG"

// ConfigPath is the Claude Code document that stores per-project
// hasTrustDialogAccepted. Empty when home cannot be resolved or the
// operator has disabled the write.
func ConfigPath() string {
	if p := strings.TrimSpace(os.Getenv(ConfigEnv)); p != "" {
		if strings.EqualFold(p, "off") {
			return ""
		}
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".claude.json")
}

// workspaceTrustMarkers are the Claude Code TUI / warning strings that mean
// the composer is blocked on workspace trust. Matched case-insensitively.
var workspaceTrustMarkers = []string{
	"quick safety check",
	"is this a project you created or one you trust",
	"yes, i trust this folder",
	"i trust this folder",
	"this workspace has not been trusted",
	"workspace has not been trusted",
}

// IsDialog reports whether msg is Claude's workspace-trust block — the
// TUI "Quick safety check" or the "workspace has not been trusted" warning
// that appears in the same last-frame position on a no_composer stall.
func IsDialog(msg string) bool {
	low := strings.ToLower(msg)
	for _, m := range workspaceTrustMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// PrepareLaunch writes workspace trust for a Claude seat's workdir before
// Launch. No-op for other providers, empty workdirs, and (during go test)
// the owner's real ~/.claude.json.
func PrepareLaunch(reg *claudia.Registry, name string) {
	PrepareLaunchAt(reg, name, ConfigPath())
}

// PrepareLaunchAt is PrepareLaunch against an explicit config path (tests).
func PrepareLaunchAt(reg *claudia.Registry, name, configPath string) {
	if reg == nil || strings.TrimSpace(name) == "" || strings.TrimSpace(configPath) == "" {
		return
	}
	d := reg.Def(name)
	if d == nil || d.Provider != claudia.ProviderClaude {
		return
	}
	wd := strings.TrimSpace(d.WorkDir)
	if wd == "" {
		return
	}
	if _, err := Accept(configPath, wd); err != nil {
		slog.Warn("claude workspace trust pre-accept failed",
			"component", "claudetrust", "name", name, "workdir", wd, "err", err)
	}
}

// Accept sets projects[workdir].hasTrustDialogAccepted=true in the Claude
// config, creating the file or project entry when missing. changed is false
// when the flag is already true — then nothing is written.
func Accept(configPath, workdir string) (changed bool, err error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return false, nil
	}
	if !allowWrite(configPath) {
		return false, nil
	}
	workdir = normalizeWorkdir(workdir)
	if workdir == "" {
		return false, fmt.Errorf("workdir required")
	}

	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read %s: %w", configPath, err)
	}
	if _, changed, err = AcceptBytes(data, workdir); err != nil {
		return false, err
	} else if !changed && err == nil && data != nil {
		return false, nil
	}

	unlock, err := lockFile(configPath + ".jevons-lock")
	if err != nil {
		return false, err
	}
	defer unlock()

	fresh, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("re-read %s: %w", configPath, err)
	}
	out, changed, err := AcceptBytes(fresh, workdir)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	if err := writeAtomic(configPath, out); err != nil {
		return false, err
	}
	return true, nil
}

// AcceptBytes is the pure merge: one project key, every other member kept
// as raw JSON. Empty or missing documents become {}.
func AcceptBytes(cfg []byte, workdir string) (out []byte, changed bool, err error) {
	workdir = normalizeWorkdir(workdir)
	if workdir == "" {
		return nil, false, fmt.Errorf("workdir required")
	}
	if len(bytes.TrimSpace(cfg)) == 0 {
		cfg = []byte("{}")
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(cfg, &doc); err != nil {
		return nil, false, fmt.Errorf("parse claude config: %w", err)
	}
	projects := map[string]json.RawMessage{}
	if raw, ok := doc["projects"]; ok && len(bytes.TrimSpace(raw)) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if err := json.Unmarshal(raw, &projects); err != nil {
			return nil, false, fmt.Errorf("parse projects: %w", err)
		}
	}
	proj := map[string]json.RawMessage{}
	if raw, ok := projects[workdir]; ok && len(bytes.TrimSpace(raw)) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if err := json.Unmarshal(raw, &proj); err != nil {
			return nil, false, fmt.Errorf("parse project %q: %w", workdir, err)
		}
	}
	if projectAccepted(proj) {
		return cfg, false, nil
	}
	proj["hasTrustDialogAccepted"] = json.RawMessage("true")
	encoded, err := json.Marshal(proj)
	if err != nil {
		return nil, false, fmt.Errorf("encode project %q: %w", workdir, err)
	}
	projects[workdir] = encoded
	projEncoded, err := json.Marshal(projects)
	if err != nil {
		return nil, false, fmt.Errorf("encode projects: %w", err)
	}
	doc["projects"] = projEncoded
	out, err = json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("encode claude config: %w", err)
	}
	return append(out, '\n'), true, nil
}

// Accepted reports whether cfg already has hasTrustDialogAccepted=true for
// workdir. Malformed documents are not accepted.
func Accepted(cfg []byte, workdir string) bool {
	workdir = normalizeWorkdir(workdir)
	if workdir == "" || len(bytes.TrimSpace(cfg)) == 0 {
		return false
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(cfg, &doc); err != nil {
		return false
	}
	raw, ok := doc["projects"]
	if !ok {
		return false
	}
	var projects map[string]json.RawMessage
	if err := json.Unmarshal(raw, &projects); err != nil {
		return false
	}
	projRaw, ok := projects[workdir]
	if !ok {
		return false
	}
	var proj map[string]json.RawMessage
	if err := json.Unmarshal(projRaw, &proj); err != nil {
		return false
	}
	return projectAccepted(proj)
}

func projectAccepted(proj map[string]json.RawMessage) bool {
	raw, ok := proj["hasTrustDialogAccepted"]
	if !ok {
		return false
	}
	var accepted bool
	if err := json.Unmarshal(raw, &accepted); err != nil {
		return false
	}
	return accepted
}

func normalizeWorkdir(workdir string) string {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return ""
	}
	return filepath.Clean(workdir)
}

// allowWrite refuses to mutate the owner's real ~/.claude.json from inside
// `go test` unless the test pointed ConfigEnv at that path on purpose.
func allowWrite(path string) bool {
	if !testing.Testing() {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return true
	}
	homeCfg := filepath.Clean(filepath.Join(home, ".claude.json"))
	if filepath.Clean(path) != homeCfg {
		return true
	}
	return strings.TrimSpace(os.Getenv(ConfigEnv)) == path
}

func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir for %s: %w", path, err)
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("create temp beside %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if _, statErr := os.Stat(tmpName); statErr == nil {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp for %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp for %s: %w", path, err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("chmod temp for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename onto %s: %w", path, err)
	}
	return nil
}
