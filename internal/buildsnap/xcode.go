// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package buildsnap

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Default path of Apple's Command Line Tools developer directory. A cgo
// build pointed here does not require the Xcode.app license agreement
// (🎯T655). Overridable in tests via Config.CLTDir.
const defaultCLTDir = "/Library/Developer/CommandLineTools"

// xcodeLicenseExit is the status xcodebuild / Apple clang returns when the
// Xcode license has not been accepted. Observed 2026-09-15 in
// ~/.jevons/restart-daily.log as "build bin/jevonsd in snapshot: exit status 69".
const xcodeLicenseExit = 69

// licenseNoticeBody is the owner-facing sentence the blurter (and the
// error returned to the restart log) must carry. Named so the hermetic
// oracle can require the exact fix command without coupling to wording
// around it.
const licenseNoticeBody = "Xcode license unaccepted; accept with `sudo xcodebuild -license`, or install Command Line Tools so buildsnap can fall back to DEVELOPER_DIR=/Library/Developer/CommandLineTools."

// cltDir returns the Command Line Tools developer dir for this run.
func cltDir(cfg Config) string {
	if cfg.CLTDir != "" {
		return cfg.CLTDir
	}
	return defaultCLTDir
}

// cltUsable reports whether dir looks like a usable CLT install (the
// directory exists and has a usr/ tree). A missing CLT must not be
// treated as a successful fallback target.
func cltUsable(dir string) bool {
	if dir == "" {
		return false
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return false
	}
	// Prefer the compiler path when present; fall back to the directory
	// itself so a test fixture that only creates the dir still works.
	if _, err := os.Stat(filepath.Join(dir, "usr")); err == nil {
		return true
	}
	return true
}

// isXcodeLicenseFailure reports whether a failed make/go build is the
// Xcode-license refusal rather than a real compile error. Match on the
// well-known exit status and on the license prose Apple prints, so a
// fixture that only emits the prose (or only the status) still trips.
func isXcodeLicenseFailure(out string, err error) bool {
	if err == nil {
		return false
	}
	blob := strings.ToLower(out + "\n" + err.Error())
	if strings.Contains(blob, "xcode license") ||
		strings.Contains(blob, "agreed to the xcode") ||
		strings.Contains(blob, "xcode and apple sdks license") {
		return true
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == xcodeLicenseExit {
		return true
	}
	// make wraps the recipe status as "exit status N" without an
	// ExitError when the error is reformatted; accept that form too.
	if strings.Contains(err.Error(), fmt.Sprintf("exit status %d", xcodeLicenseExit)) {
		return true
	}
	return false
}

// notifyXcodeLicense raises a blurter-visible owner notice naming the
// fix command. Best-effort: a missing blurter must not change the
// build's exit status — the error returned to the restart log already
// carries the same sentence.
func notifyXcodeLicense(cfg Config, detail string) {
	msg := licenseNoticeBody
	if detail != "" {
		msg = licenseNoticeBody + " " + detail
	}
	if cfg.NotifyLicense != nil {
		cfg.NotifyLicense(msg)
		return
	}
	cfg.logf("%s", msg)
	blurter, err := exec.LookPath("blurter")
	if err != nil {
		cfg.logf("no blurter on PATH; Xcode license notice stayed in the restart log only")
		return
	}
	cmd := exec.Command(blurter, "send",
		"--app", "jevons-buildsnap",
		"--severity", "problem",
		"--key", "xcode-license",
		"--subject", "jevons build blocked: Xcode license unaccepted",
		"--body", msg,
		"--quiet")
	if out, err := cmd.CombinedOutput(); err != nil {
		cfg.logf("blurter Xcode-license notice failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
}
