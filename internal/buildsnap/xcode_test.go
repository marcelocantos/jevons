// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package buildsnap_test

// 🎯T655: when the Xcode developer dir refuses a build because the license
// is unaccepted, buildsnap must fall back to Command Line Tools (or name
// `sudo xcodebuild -license` out of band). The fixture Makefile below is
// the hermetic stand-in for Apple's compiler: it exits 69 with the license
// prose unless DEVELOPER_DIR equals the real Command Line Tools path.
//
// The CLT path must be the real one on this host. macOS's /usr/bin/make is
// an xcrun shim: pointing DEVELOPER_DIR at a temp dir makes even `make`
// fail before the recipe runs, which is not the failure under test.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/buildsnap"
)

// Real CLT path — the same string production uses, and the acceptance
// criterion names it explicitly.
const realCLTDir = "/Library/Developer/CommandLineTools"

// licenseFixtureMakefile fails with the Xcode-license signature unless
// DEVELOPER_DIR equals realCLTDir. Driving the fallback with that exact
// path is the hermetic oracle the target asks for.
const licenseFixtureMakefile = `bin/artifact: src.txt
	@mkdir -p bin
	@if [ "$${DEVELOPER_DIR}" != "/Library/Developer/CommandLineTools" ]; then \
		echo "You have not agreed to the Xcode license agreements. Please run 'sudo xcodebuild -license' from within a Terminal window to review and agree to the Xcode and Apple SDKs license." >&2; \
		exit 69; \
	fi
	@cp src.txt bin/artifact
`

func requireCLT(t *testing.T) {
	t.Helper()
	fi, err := os.Stat(realCLTDir)
	if err != nil || !fi.IsDir() {
		t.Skipf("Command Line Tools not installed at %s; cannot drive the 🎯T655 fallback hermetically", realCLTDir)
	}
}

func newLicenseRepo(t *testing.T) string {
	t.Helper()
	root := newRepo(t)
	write(t, filepath.Join(root, "Makefile"), licenseFixtureMakefile)
	mustRun(t, root, "git", "add", "Makefile")
	mustRun(t, root, "git", "commit", "-q", "-m", "license fixture")
	return root
}

// TestXcodeLicenseFallsBackToCLT is the acceptance oracle for 🎯T655: a
// build that fails under the ambient (Xcode) developer dir with the
// license error must succeed when retried with
// DEVELOPER_DIR=/Library/Developer/CommandLineTools.
func TestXcodeLicenseFallsBackToCLT(t *testing.T) {
	requireCLT(t)
	root := newLicenseRepo(t)

	var notices []string
	_, err := buildsnap.Run(buildsnap.Config{
		RepoRoot: root,
		SnapDir:  filepath.Join(t.TempDir(), "snap"),
		Target:   "bin/artifact",
		Artifact: "bin/artifact",
		Dest:     filepath.Join(root, "bin", "artifact"),
		// CLTDir empty → production default = realCLTDir.
		Log: func(s string) { t.Log(s) },
		NotifyLicense: func(msg string) {
			notices = append(notices, msg)
		},
	})
	if err != nil {
		t.Fatalf("license fallback should have built under CLT: %v", err)
	}
	if got := artifact(t, root); got != goodSrc {
		t.Errorf("artifact = %q, want %q", got, goodSrc)
	}
	// Successful fallback is not an owner-facing problem.
	if len(notices) != 0 {
		t.Errorf("blurter fired on a successful CLT fallback: %v", notices)
	}
}

// TestXcodeLicenseWithoutCLTNotifiesOwner covers the refuse path: when
// the CLT directory is missing, the build must fail AND the notice must
// name `sudo xcodebuild -license` so the owner can act out of band.
//
// CLTDir points at a path that does not exist; the fallback is refused
// before a second make is attempted (so macOS xcrun is never handed a
// bogus DEVELOPER_DIR).
func TestXcodeLicenseWithoutCLTNotifiesOwner(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-clt")
	root := newLicenseRepo(t)

	var notices []string
	_, err := buildsnap.Run(buildsnap.Config{
		RepoRoot: root,
		SnapDir:  filepath.Join(t.TempDir(), "snap"),
		Target:   "bin/artifact",
		Artifact: "bin/artifact",
		Dest:     filepath.Join(root, "bin", "artifact"),
		CLTDir:   missing,
		Log:      func(s string) { t.Log(s) },
		NotifyLicense: func(msg string) {
			notices = append(notices, msg)
		},
	})
	if err == nil {
		t.Fatal("expected failure when CLT fallback is unavailable")
	}
	errText := err.Error()
	if !strings.Contains(errText, "sudo xcodebuild -license") {
		t.Errorf("error must name the owner fix; got: %v", err)
	}
	if len(notices) != 1 {
		t.Fatalf("want exactly one license notice, got %d: %v", len(notices), notices)
	}
	if !strings.Contains(notices[0], "sudo xcodebuild -license") {
		t.Errorf("notice must name sudo xcodebuild -license; got %q", notices[0])
	}
}
