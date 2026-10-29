// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package claudetrust pre-accepts Claude Code's per-workdir trust dialog
// for seats jevons mints, so a fleet seat cannot be born behind a modal
// that only a human at a keyboard can clear (🎯T709).
//
// Claude Code asks once per directory — "Quick safety check: Is this a
// project you created or one you trust?" — and records the answer in
// ~/.claude.json under projects.<abs path>.hasTrustDialogAccepted. A
// tmux-driven seat has nobody to answer it: the composer never draws,
// claudia's launch handshake times out, and the seat is reaped. On
// 2026-09-20 that silently killed ge-po repeatedly on a freshly-cloned
// squz workdir, and the owner saw only reaped_held on send.
//
// This package is the auto-resolve half of T709; internal/agenterr's
// ClassWorkspaceTrust is the tell-the-owner half, for the workdirs this
// package deliberately refuses.
//
// Scope, deliberately narrow. Trust is a safety boundary: accepting it
// for a directory the owner never pointed jevons at would hand arbitrary
// cloned code an agent that reads it. So EnsureAccepted only ever writes
// for a workdir under one of the owner roots (see WithinOwnerRoots), and
// it never clears an existing answer — only ever adds a missing one.
//
// Relation to 🎯T464: that target forbids jevons writing MCP server
// registrations into provider HOME config, because fleet control follows
// the agent (AgentDef.MCPServers), not the directory. Trust is the
// opposite shape — it IS per-directory state, Claude Code offers no
// per-launch flag for it, and there is no AgentDef field that carries it.
// This package touches that one key and nothing else, and mcpattach's
// scrub still owns the mcpServers side.
package claudetrust

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// trustKey is Claude Code's own spelling of the per-project answer.
const trustKey = "hasTrustDialogAccepted"

// projectsKey is the top-level map keyed by absolute workdir.
const projectsKey = "projects"

// ConfigPath is the Claude Code config a launched seat reads. Empty when
// the home directory cannot be determined, which callers treat as
// "cannot tell" rather than "not trusted" — guessing either way here
// would either skip a needed write or write to the wrong file.
func ConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude.json")
}

// DefaultOwnerRoots are the directories whose contents are the owner's
// own checkouts. Everything jevons mints a seat on lives under one of
// them; anything else is someone else's code and gets the owner action
// instead of an automatic yes.
func DefaultOwnerRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, "work")}
}

// WithinOwnerRoots reports whether workdir is inside one of roots.
//
// Pure, and it compares cleaned absolute paths with a separator-aware
// prefix so that ~/workshop is not read as being under ~/work. A
// relative workdir, an empty one, or an empty root set is false: this
// answers "may jevons say yes on the owner's behalf", and the safe
// answer to a question it cannot resolve is no.
func WithinOwnerRoots(workdir string, roots []string) bool {
	if workdir == "" || !filepath.IsAbs(workdir) {
		return false
	}
	dir := filepath.Clean(workdir)
	for _, root := range roots {
		if root == "" || !filepath.IsAbs(root) {
			continue
		}
		root = filepath.Clean(root)
		if dir == root || strings.HasPrefix(dir, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// Accepted reports whether the config document already records trust for
// workdir. A malformed document, a missing projects map, or a missing
// entry are all false — an unreadable answer is not an answer.
func Accepted(doc []byte, workdir string) bool {
	var top map[string]json.RawMessage
	if json.Unmarshal(doc, &top) != nil {
		return false
	}
	var projects map[string]json.RawMessage
	if json.Unmarshal(top[projectsKey], &projects) != nil {
		return false
	}
	var project map[string]json.RawMessage
	if json.Unmarshal(projects[filepath.Clean(workdir)], &project) != nil {
		return false
	}
	var accepted bool
	if json.Unmarshal(project[trustKey], &accepted) != nil {
		return false
	}
	return accepted
}

// SetAccepted returns doc with trust recorded for workdir, preserving
// every other member of the document, of the projects map, and of that
// project's own entry.
//
// Held as raw members on purpose (the 🎯T376 shape): ~/.claude.json is
// hot shared state that Claude Code itself rewrites, it carries far more
// than jevons models, and a round-trip through a typed struct would drop
// whatever this build does not know about — including the seat's own
// history. Only the one boolean is authored here.
//
// An empty or blank doc is treated as an empty object, so a machine with
// no config yet gets a well-formed one rather than a parse error.
func SetAccepted(doc []byte, workdir string) ([]byte, error) {
	dir := filepath.Clean(workdir)
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("claudetrust: workdir %q is not absolute", workdir)
	}

	top := map[string]json.RawMessage{}
	if len(strings.TrimSpace(string(doc))) > 0 {
		if err := json.Unmarshal(doc, &top); err != nil {
			return nil, fmt.Errorf("claudetrust: parse config: %w", err)
		}
	}

	projects := map[string]json.RawMessage{}
	if raw, ok := top[projectsKey]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &projects); err != nil {
			return nil, fmt.Errorf("claudetrust: parse %s: %w", projectsKey, err)
		}
	}

	project := map[string]json.RawMessage{}
	if raw, ok := projects[dir]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &project); err != nil {
			return nil, fmt.Errorf("claudetrust: parse project %q: %w", dir, err)
		}
	}

	project[trustKey] = json.RawMessage("true")

	encoded, err := json.Marshal(project)
	if err != nil {
		return nil, err
	}
	projects[dir] = encoded
	if encoded, err = json.Marshal(projects); err != nil {
		return nil, err
	}
	top[projectsKey] = encoded
	return json.MarshalIndent(top, "", "  ")
}

// Outcome says what EnsureAccepted did, so the caller can log it and so
// a refusal is distinguishable from a no-op. "Cannot tell" is its own
// answer for the same reason ConfigPath returns "".
type Outcome string

const (
	// OutcomeAlready: the config already recorded trust; nothing written.
	OutcomeAlready Outcome = "already_trusted"
	// OutcomeGranted: this call added the missing answer.
	OutcomeGranted Outcome = "granted"
	// OutcomeRefused: the workdir is outside the owner roots. The seat is
	// launched anyway; if the modal appears, agenterr names the action.
	OutcomeRefused Outcome = "refused_outside_owner_roots"
	// OutcomeUnknown: the config path or document could not be read.
	OutcomeUnknown Outcome = "unknown"
)

// EnsureAccepted records trust for workdir in the config at path when
// the workdir is under one of roots and the answer is missing.
//
// It writes atomically (temp file in the same directory, then rename) so
// a concurrent Claude Code read never sees a half-written config, and it
// keeps the existing file's mode — or 0600 for a new one, since this
// file holds the owner's provider state.
//
// Errors are returned rather than swallowed, but every outcome is
// survivable by the caller: the worst case of not writing is the modal
// the owner is now told about by name.
func EnsureAccepted(path, workdir string, roots []string) (Outcome, error) {
	if path == "" {
		return OutcomeUnknown, fmt.Errorf("claudetrust: no config path")
	}
	if !WithinOwnerRoots(workdir, roots) {
		return OutcomeRefused, nil
	}
	dir := filepath.Clean(workdir)

	doc, err := os.ReadFile(path)
	mode := os.FileMode(0o600)
	switch {
	case err == nil:
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode().Perm()
		}
		if Accepted(doc, dir) {
			return OutcomeAlready, nil
		}
	case os.IsNotExist(err):
		doc = nil
	default:
		return OutcomeUnknown, fmt.Errorf("claudetrust: read %s: %w", path, err)
	}

	next, err := SetAccepted(doc, dir)
	if err != nil {
		return OutcomeUnknown, err
	}
	if err := atomicWrite(path, next, mode); err != nil {
		return OutcomeUnknown, err
	}
	return OutcomeGranted, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".jevons-*")
	if err != nil {
		return fmt.Errorf("claudetrust: temp for %s: %w", path, err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("claudetrust: write %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("claudetrust: close %s: %w", name, err)
	}
	if err := os.Chmod(name, mode); err != nil {
		return fmt.Errorf("claudetrust: chmod %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("claudetrust: rename onto %s: %w", path, err)
	}
	return nil
}
