// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A guard that has to be installed by hand is not installed (🎯T377).
//
// The refusal in scope.go only fires in a clone whose .git/hooks/pre-commit
// exists, and git never populates that from the tree — a fresh clone commits
// the shared index by default, which is the very thing the target rules out.
// Documenting `cp scripts/hooks/pre-commit .git/hooks/` puts the guard behind
// a step a worker must remember, and the failure mode this target exists to
// remove is precisely a step a worker did not remember.
//
// So `make` installs it. The interesting decision is not the copy but what to
// do about a pre-commit hook that is already there: replacing one blindly
// would make this build step destroy work belonging to whoever wrote it.
// Hooks this repo wrote carry HookMarker and may be refreshed; anything else
// is left exactly as found and reported loudly, because a clone where the
// guard is inert must say so rather than look installed.

// HookMarker appears in scripts/hooks/pre-commit and identifies an installed
// copy as this repo's, so a later `make` may refresh it in place. Any hook
// without it belongs to someone else.
const HookMarker = "🎯T377"

// hookMode is what git requires of a hook it will exec.
const hookMode = 0o755

// InstallOutcome is what InstallHook found and did.
type InstallOutcome int

const (
	// HookInstalled: no pre-commit hook was present, and ours now is.
	HookInstalled InstallOutcome = iota
	// HookUpdated: an older copy of ours was present and has been refreshed.
	HookUpdated
	// HookCurrent: our hook was already installed, byte for byte.
	HookCurrent
	// HookForeign: a pre-commit hook this repo did not write is installed.
	// It was left untouched, and the guard is therefore not active.
	HookForeign
)

func (o InstallOutcome) String() string {
	switch o {
	case HookInstalled:
		return "installed"
	case HookUpdated:
		return "updated"
	case HookCurrent:
		return "current"
	case HookForeign:
		return "foreign"
	}
	return "unknown"
}

// Active reports whether the guard runs on the next commit in this clone.
func (o InstallOutcome) Active() bool { return o != HookForeign }

// ClassifyExisting decides what may be done with the bytes already sitting at
// the hook path. Pure, so the one judgement call here — "is this hook ours to
// replace?" — is decided by a test rather than by a build script.
func ClassifyExisting(existing, ours []byte) InstallOutcome {
	switch {
	case bytes.Equal(existing, ours):
		return HookCurrent
	case bytes.Contains(existing, []byte(HookMarker)):
		return HookUpdated
	default:
		return HookForeign
	}
}

// InstallHook copies source into hooksDir/pre-commit unless a hook that is
// not ours is already there. hooksDir is the directory git will actually look
// in — the caller resolves core.hooksPath, since a redirected hooks path makes
// an install into .git/hooks inert.
//
// A foreign hook is not an error: it is a true statement about the clone, and
// the caller reports it. Everything else that goes wrong is an error, because
// a guard that could not be installed must not be mistaken for one that was.
func InstallHook(hooksDir, source string) (InstallOutcome, error) {
	ours, err := os.ReadFile(source)
	if err != nil {
		return HookForeign, fmt.Errorf("reading %s: %w", source, err)
	}
	if !bytes.Contains(ours, []byte(HookMarker)) {
		// Without the marker the next install could not recognise its own
		// work and would refuse to refresh it ever again.
		return HookForeign, fmt.Errorf("%s does not carry %s, so an installed copy could never be updated", source, HookMarker)
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return HookForeign, fmt.Errorf("creating %s: %w", hooksDir, err)
	}

	dest := filepath.Join(hooksDir, "pre-commit")
	outcome := HookInstalled
	switch existing, err := os.ReadFile(dest); {
	case err == nil:
		if outcome = ClassifyExisting(existing, ours); outcome == HookForeign || outcome == HookCurrent {
			return outcome, nil
		}
	case !os.IsNotExist(err):
		return HookForeign, fmt.Errorf("reading %s: %w", dest, err)
	}

	// Write-and-rename: a worker committing while `make` runs execs either
	// the old hook or the new one, never a half-written file.
	tmp, err := os.CreateTemp(hooksDir, ".pre-commit.*")
	if err != nil {
		return HookForeign, fmt.Errorf("creating a temporary file in %s: %w", hooksDir, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(ours); err != nil {
		tmp.Close()
		return HookForeign, fmt.Errorf("writing %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return HookForeign, fmt.Errorf("closing %s: %w", tmp.Name(), err)
	}
	if err := os.Chmod(tmp.Name(), hookMode); err != nil {
		return HookForeign, fmt.Errorf("making %s executable: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return HookForeign, fmt.Errorf("installing %s: %w", dest, err)
	}
	return outcome, nil
}

// InstallReport is what the worker reads on stderr. A foreign hook gets the
// whole story, since that clone is unguarded and nothing else will say so.
func InstallReport(outcome InstallOutcome, hooksDir string) string {
	dest := filepath.Join(hooksDir, "pre-commit")
	switch outcome {
	case HookCurrent:
		return ""
	case HookInstalled, HookUpdated:
		return fmt.Sprintf("commitscope: shared-index commit guard %s at %s (🎯T377).\n", outcome, dest)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "commitscope: %s already exists and was not written by this repo — LEAVING IT ALONE.\n", dest)
	b.WriteString("The shared-index commit guard is NOT active in this clone: a bare `git commit`\n")
	b.WriteString("here can still sweep another worker's staged hunks into your commit (🎯T377).\n")
	b.WriteString("Chain the guard from your hook, or install it over yours with:\n")
	b.WriteString("  cp scripts/hooks/pre-commit " + dest + "\n")
	return b.String()
}
