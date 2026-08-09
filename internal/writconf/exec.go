// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package writconf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Executor runs argv under a compiled writ manifest (real CLI or test double).
type Executor interface {
	Exec(ctx context.Context, args *ExecArgs) (*ExecResult, error)
}

// ExecArgs configures one confined run.
type ExecArgs struct {
	Manifest *Manifest
	// ManifestPath, when set, is used instead of writing a temp file.
	ManifestPath string
	Argv         []string
	WorkDir      string
	Agent        string
	// Timeout bounds the child; zero = 60s default for product path.
	Timeout time.Duration
}

// ExecResult is the outcome of a confined execution.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	// Denied is true when undeclared access was refused (egress 403 / sandbox deny).
	Denied bool
	// Event is a structured deny/drift observation when detectable.
	Event *Event
	// ManifestPath is the on-disk manifest used for the run.
	ManifestPath string
	// UsedWrit is the resolved writ binary path.
	UsedWrit string
}

// CLIExecutor shells out to the writ binary (preferred product substrate).
type CLIExecutor struct {
	// Bin is the writ executable. Empty resolves via ResolveWritBin.
	Bin string
	// LookPath overrides exec.LookPath (tests).
	LookPath func(file string) (string, error)
}

// ResolveWritBin finds a writ CLI: PATH, then common local checkouts.
func ResolveWritBin() string {
	if p, err := exec.LookPath("writ"); err == nil {
		return p
	}
	candidates := []string{
		filepath.Join("..", "writ", "bin", "writ"),
		filepath.Join(os.Getenv("HOME"), "work", "github.com", "marcelocantos", "writ", "bin", "writ"),
		"/Users/marcelo/work/github.com/marcelocantos/writ/bin/writ",
	}
	// Also relative to this module if running from repo root.
	if wd, err := os.Getwd(); err == nil {
		candidates = append([]string{
			filepath.Join(wd, "..", "writ", "bin", "writ"),
			filepath.Join(wd, "bin", "writ"),
		}, candidates...)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// Available reports whether a writ binary can be resolved.
func (e *CLIExecutor) Available() bool {
	return e.resolve() != ""
}

func (e *CLIExecutor) resolve() string {
	if e != nil && strings.TrimSpace(e.Bin) != "" {
		return e.Bin
	}
	if e != nil && e.LookPath != nil {
		if p, err := e.LookPath("writ"); err == nil {
			return p
		}
	}
	return ResolveWritBin()
}

// Exec validates the manifest, writes it if needed, and runs writ exec.
func (e *CLIExecutor) Exec(ctx context.Context, args *ExecArgs) (*ExecResult, error) {
	if args == nil {
		return nil, fmt.Errorf("writconf: nil exec args")
	}
	if len(args.Argv) == 0 {
		return nil, fmt.Errorf("writconf: empty argv")
	}
	bin := e.resolve()
	if bin == "" {
		return nil, fmt.Errorf("writconf: writ binary not found on PATH or local checkout")
	}

	manPath := strings.TrimSpace(args.ManifestPath)
	var cleanup func()
	if manPath == "" {
		if args.Manifest == nil {
			return nil, fmt.Errorf("writconf: manifest required")
		}
		data, err := MarshalJSONDocument(args.Manifest)
		if err != nil {
			return nil, err
		}
		dir, err := os.MkdirTemp("", "jevons-writ-*")
		if err != nil {
			return nil, err
		}
		manPath = filepath.Join(dir, "manifest.json")
		if err := os.WriteFile(manPath, data, 0o600); err != nil {
			_ = os.RemoveAll(dir)
			return nil, err
		}
		cleanup = func() { _ = os.RemoveAll(dir) }
	}
	if cleanup != nil {
		defer cleanup()
	}

	timeout := args.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmdArgs := []string{"exec", "--manifest", manPath, "--"}
	cmdArgs = append(cmdArgs, args.Argv...)
	cmd := exec.CommandContext(runCtx, bin, cmdArgs...)
	if args.WorkDir != "" {
		cmd.Dir = args.WorkDir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := &ExecResult{
		Stdout:       stdout.String(),
		Stderr:       stderr.String(),
		ManifestPath: manPath,
		UsedWrit:     bin,
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	} else if err != nil {
		res.ExitCode = -1
	}

	// Detect structured egress denials in child output (curl body / writ errors).
	if ev := ParseDenialOutput(res.Stdout + "\n" + res.Stderr); ev != nil {
		ev.Agent = args.Agent
		res.Denied = true
		res.Event = ev
	}
	// Pure preflight classification for high-risk argv hosts (even if writ allowed compile).
	if res.Event == nil && args.Manifest != nil {
		if host := hostFromArgv(args.Argv); host != "" {
			if ev := ClassifyAccess(args.Manifest, args.Agent, host, "", "read"); ev != nil {
				// Only stamp when child also failed — avoid false deny on allow path.
				if err != nil || res.ExitCode != 0 {
					res.Denied = true
					res.Event = ev
				}
			}
		}
	}
	if err != nil && res.Denied {
		return res, nil // structured deny is not a transport failure
	}
	if err != nil {
		if runCtx.Err() != nil {
			return res, fmt.Errorf("writconf: exec timeout: %w", runCtx.Err())
		}
		return res, fmt.Errorf("writconf: writ exit %d: %w", res.ExitCode, err)
	}
	return res, nil
}

// ParseDenialOutput extracts a missing_capability / sandbox denial from text.
func ParseDenialOutput(text string) *Event {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	// Prefer JSON Denial objects from writ egress proxy.
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] != '{' {
			continue
		}
		var d struct {
			Error         string `json:"error"`
			Host          string `json:"host"`
			Path          string `json:"path"`
			ManifestField string `json:"manifest_field"`
			Message       string `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			continue
		}
		if d.Error == "missing_capability" || strings.Contains(d.Message, "not declared") {
			return &Event{
				Kind:          KindDenyNet,
				Host:          d.Host,
				Path:          d.Path,
				ManifestField: d.ManifestField,
				Message:       firstNonEmpty(d.Message, "undeclared network access"),
			}
		}
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "missing_capability") ||
		strings.Contains(lower, "manifest_field") && strings.Contains(lower, "not declared") ||
		strings.Contains(lower, "sandbox-exec") && strings.Contains(lower, "deny") {
		return &Event{
			Kind:    KindDenyNet,
			Message: "confinement denied undeclared access",
		}
	}
	return nil
}

func hostFromArgv(argv []string) string {
	for _, a := range argv {
		if strings.HasPrefix(a, "http://") || strings.HasPrefix(a, "https://") {
			return hostFromFetch(a)
		}
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// PureExecutor is a hermetic Executor that never shells out: it applies
// ClassifyAccess against the first URL/path in argv and allows the rest.
// Used in unit tests and as a fail-soft path when writ is unavailable.
type PureExecutor struct{}

// Exec implements Executor without OS confinement (oracle for policy only).
func (PureExecutor) Exec(_ context.Context, args *ExecArgs) (*ExecResult, error) {
	if args == nil || len(args.Argv) == 0 {
		return nil, fmt.Errorf("writconf: empty args")
	}
	host := hostFromArgv(args.Argv)
	path := ""
	for _, a := range args.Argv {
		if strings.HasPrefix(a, "/") || strings.HasPrefix(a, "~/") {
			path = a
			break
		}
	}
	if ev := ClassifyAccess(args.Manifest, args.Agent, host, path, "read"); ev != nil {
		return &ExecResult{
			ExitCode: 1,
			Denied:   true,
			Event:    ev,
			Stderr:   fmt.Sprintf("pure deny: %s", ev.Message),
		}, nil
	}
	return &ExecResult{
		ExitCode: 0,
		Stdout:   "ok",
	}, nil
}
