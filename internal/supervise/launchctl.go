// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// This is the one place that talks to launchd, so the daemon's
// reinstatement path (🎯T405) and the watchdog's own -install use the
// same code and cannot drift into two different definitions of loaded.

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
	if runtime.GOOS != "darwin" {
		return false, nil
	}
	out, err := exec.Command("launchctl", "print", AgentDomain()+"/"+label).CombinedOutput()
	if err == nil {
		return true, nil
	}
	// "Could not find service" is the answer, not a failure to get one.
	if strings.Contains(string(out), "Could not find service") {
		return false, nil
	}
	return false, fmt.Errorf("supervise: launchctl print %s: %w: %s", label, err, strings.TrimSpace(string(out)))
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
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("supervise: launchd agents are macOS only")
	}
	domain := AgentDomain()
	out, err := exec.Command("launchctl", "bootstrap", domain, plistPath).CombinedOutput()
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
	if out, err := exec.Command("launchctl", "bootout", domain+"/"+label).CombinedOutput(); err != nil {
		return fmt.Errorf("supervise: bootout %s: %w: %s", label, err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("launchctl", "bootstrap", domain, plistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("supervise: re-bootstrap %s: %w: %s", label, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// UnloadAgent removes the job from launchd. A job that is not there is
// not an error — the caller asked for it gone.
func UnloadAgent(label string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("supervise: launchd agents are macOS only")
	}
	out, err := exec.Command("launchctl", "bootout", AgentDomain()+"/"+label).CombinedOutput()
	if err == nil || strings.Contains(string(out), "Could not find service") {
		return nil
	}
	return fmt.Errorf("supervise: bootout %s: %w: %s", label, err, strings.TrimSpace(string(out)))
}
