// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// This is the one place that talks to launchd, so the daemon's
// reinstatement path (🎯T405) and the watchdog's own -install use the
// same code and cannot drift into two different definitions of loaded.

// LaunchctlCommand is the launchd control tool. No other Go file in the
// module names it (🎯T1097, ratcheted by internal/launchdfake): every
// launchctl exec goes through Launchctl.
const LaunchctlCommand = "launchctl"

// LaunchdFakePackage is the package a test binary imports for effect to
// answer for launchd (🎯T1097).
const LaunchdFakePackage = "github.com/marcelocantos/jevons/internal/launchdfake"

// launchctl runs one launchctl command and returns its combined output.
// Every call in the module goes through it, so a test can answer for
// launchd instead of reaching the owner's real one (🎯T1095): an
// observation or a bootstrap from a test run lands on the same gui/<uid>
// domain the development watchdog lives in.
//
// In a test binary the default refuses rather than execs (🎯T1097). A
// test binary that links this package is required to install a fake
// (internal/launchdfake, or this package's own export_test.go), and the
// ratchet that requires it runs in another binary — so without this the
// first run of a binary that forgot the import would already have reached
// the owner's launchd before anything failed.
var launchctl = func(args ...string) ([]byte, error) {
	if testing.Testing() {
		return []byte("launchctl refused in a test binary"), fmt.Errorf(
			"supervise: launchctl %s refused: a test binary reached for the owner's launchd; "+
				"add a _test.go file with import _ %q", strings.Join(args, " "), LaunchdFakePackage)
	}
	return exec.Command(LaunchctlCommand, args...).CombinedOutput()
}

// Launchctl runs one launchctl command through the seam and returns its
// combined output. It is the one launchctl exec in the module, for the
// callers outside this package that need a launchctl verb the helpers
// below do not cover.
func Launchctl(args ...string) ([]byte, error) { return launchctl(args...) }

// ReplaceLaunchctl installs fn as the seam and returns the one it
// replaced. Test binaries only: it panics anywhere else, so production
// code cannot redirect launchd. Install once, before tests start
// (internal/launchdfake does it from init); swapping it under a running
// goroutine is a data race.
func ReplaceLaunchctl(fn func(args ...string) ([]byte, error)) func(args ...string) ([]byte, error) {
	if !testing.Testing() {
		panic("supervise.ReplaceLaunchctl is for test binaries only")
	}
	prev := launchctl
	launchctl = fn
	return prev
}

// haveLaunchd reports whether this platform has a launchd to talk to. A
// variable beside launchctl so a test that fakes launchd on another
// platform exercises the same paths it would on macOS.
var haveLaunchd = runtime.GOOS == "darwin"

// AgentDomain is the launchd domain the LaunchAgent lives in.
func AgentDomain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

// AgentLoaded reports whether launchd is holding the job right now.
//
// launchctl print rather than launchctl list: list matches by prefix and
// answers from a cached table, while print addresses one service in one
// domain and fails cleanly when it is absent — which is the question
// being asked. A non-darwin build reports false with no error, because
// there is no launchd to hold anything.
func AgentLoaded(label string) (bool, error) {
	if !haveLaunchd {
		return false, nil
	}
	out, err := launchctl("print", AgentDomain()+"/"+label)
	if err == nil {
		return true, nil
	}
	// "Could not find service" is the answer, not a failure to get one.
	if strings.Contains(string(out), "Could not find service") {
		return false, nil
	}
	return false, fmt.Errorf("supervise: launchctl print %s: %w: %s", label, err, strings.TrimSpace(string(out)))
}

// AgentDisabled reports whether the owner has disabled the job in its
// domain (`launchctl disable`), which is an off switch rather than a
// failure: launchd refuses to bootstrap a disabled job — "Bootstrap
// failed: 5: Input/output error" — however good the plist is (🎯T1095).
//
// print-disabled lists every service with an override in the domain, one
// per line as `"label" => disabled` (older releases print `=> true`). A
// label absent from the list has no override and is enabled.
func AgentDisabled(label string) (bool, error) {
	if !haveLaunchd {
		return false, nil
	}
	out, err := launchctl("print-disabled", AgentDomain())
	if err != nil {
		return false, fmt.Errorf("supervise: launchctl print-disabled %s: %w: %s", AgentDomain(), err, strings.TrimSpace(string(out)))
	}
	return disabledIn(string(out), label), nil
}

// disabledIn reads one label's override out of print-disabled output.
func disabledIn(out, label string) bool {
	quoted := `"` + label + `"`
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=>")
		if !ok || strings.TrimSpace(key) != quoted {
			continue
		}
		switch strings.TrimSpace(value) {
		case "disabled", "true":
			return true
		}
		return false
	}
	return false
}

// LoadAgent makes launchd hold the job described by plistPath.
//
// Bootstrap first, and only tear the running job down if that is what
// stood in the way. The obvious order — bootout then bootstrap, so a
// reinstall picks up new arguments — has a hole this target was written
// about: when the bootstrap fails, the bootout has already succeeded,
// and the machine is left with no supervisor at all while the only
// report is a line of stderr in whichever terminal ran it. That is a
// plausible reading of how the job went missing on 2026-08-10 with a
// perfectly good plist still on disk. This order cannot produce that
// state — the old job is only removed once its replacement is known to
// be loadable, and a total failure leaves whatever was working alone.
func LoadAgent(plistPath, label string) error {
	if !haveLaunchd {
		return fmt.Errorf("supervise: launchd agents are macOS only")
	}
	domain := AgentDomain()
	out, err := launchctl("bootstrap", domain, plistPath)
	if err == nil {
		return nil
	}
	// Distinguish "already there" from "would not load" by asking,
	// rather than by matching launchctl's wording, which varies by
	// macOS release and by whether the job is merely disabled.
	loaded, lerr := AgentLoaded(label)
	if lerr != nil {
		return fmt.Errorf("supervise: bootstrap %s: %w: %s", label, err, strings.TrimSpace(string(out)))
	}
	if !loaded {
		return fmt.Errorf("supervise: bootstrap %s: %w: %s", label, err, strings.TrimSpace(string(out)))
	}
	// It was in the way. Replace it so the new arguments take effect.
	if out, err := launchctl("bootout", domain+"/"+label); err != nil {
		return fmt.Errorf("supervise: bootout %s: %w: %s", label, err, strings.TrimSpace(string(out)))
	}
	if out, err := launchctl("bootstrap", domain, plistPath); err != nil {
		return fmt.Errorf("supervise: re-bootstrap %s: %w: %s", label, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// UnloadAgent removes the job from launchd. A job that is not there is
// not an error — the caller asked for it gone.
func UnloadAgent(label string) error {
	if !haveLaunchd {
		return fmt.Errorf("supervise: launchd agents are macOS only")
	}
	out, err := launchctl("bootout", AgentDomain()+"/"+label)
	if err == nil || strings.Contains(string(out), "Could not find service") {
		return nil
	}
	return fmt.Errorf("supervise: bootout %s: %w: %s", label, err, strings.TrimSpace(string(out)))
}
