// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package sticky

import (
	"fmt"
	"os/exec"
	"strings"
)

// notifierBin is the only macOS notification CLI that can *withdraw* an alert
// it posted. `osascript -e 'display notification'` can post but never remove,
// which would make the 🎯T319 auto-clear semantics unimplementable — so this
// backend requires terminal-notifier rather than degrading to osascript and
// leaving stale "stuck" alerts on the owner's screen forever.
const notifierBin = "terminal-notifier"

// notifier posts sticky alerts through terminal-notifier, one group per agent.
type notifier struct{ bin string }

// NewOSNotifier returns the macOS sticky-alert backend, or an error naming
// what is missing. The daemon logs that error and leaves the human rung
// unwired, which converge.Actuate then reports per fire — an unwired top rung
// is visible, not silent.
func NewOSNotifier() (Backend, error) {
	bin, err := exec.LookPath(notifierBin)
	if err != nil {
		return nil, fmt.Errorf("sticky: %s not found on PATH (brew install %s): %w",
			notifierBin, notifierBin, err)
	}
	return &notifier{bin: bin}, nil
}

// Show posts (or replaces, via -group) the alert. Notification Center keeps a
// grouped alert until it is removed or the owner dismisses it, which is the
// "sticky" the owner asked for: silence does not make it go away.
func (n *notifier) Show(group, title, message string) error {
	// terminal-notifier renders newlines poorly in the banner body; the alert
	// is a pointer to the chat, not the report, so flatten it.
	body := strings.Join(strings.Fields(message), " ")
	out, err := exec.Command(n.bin,
		"-title", title,
		"-message", body,
		"-group", group,
		"-ignoreDnD",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s show: %w: %s", notifierBin, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Withdraw removes the alert for this group — the 🎯T319 auto-clear.
func (n *notifier) Withdraw(group string) error {
	out, err := exec.Command(n.bin, "-remove", group).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s remove: %w: %s", notifierBin, err, strings.TrimSpace(string(out)))
	}
	return nil
}
