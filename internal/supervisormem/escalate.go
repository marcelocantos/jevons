// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervisormem

import (
	"fmt"
	"os/exec"
	"strings"
)

// BlurterSendFunc sends through blurter; tests replace this.
type BlurterSendFunc func(subject, body, severity, key string) (string, error)

// DefaultBlurterSend runs blurter from the daemon process (🎯T704 / 🎯T700).
var DefaultBlurterSend BlurterSendFunc = blurterSendCLI

func blurterSendCLI(subject, body, severity, key string) (string, error) {
	blurter, err := exec.LookPath("blurter")
	if err != nil {
		return "", fmt.Errorf("blurter not on PATH")
	}
	sev := strings.TrimSpace(severity)
	if sev == "" {
		sev = "problem"
	}
	args := []string{
		"send",
		"--app", "jevons-supervisor",
		"--severity", sev,
		"--subject", subject,
		"--body", body,
	}
	if k := strings.TrimSpace(key); k != "" {
		args = append(args, "--key", k)
	}
	out, err := exec.Command(blurter, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("blurter send: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// Escalate pages the owner through blurter with the supervisor app id.
func Escalate(subject, body, severity, key string, send BlurterSendFunc) (string, error) {
	subject = strings.TrimSpace(subject)
	body = strings.TrimSpace(body)
	if subject == "" || body == "" {
		return "", fmt.Errorf("supervisormem: subject and body required")
	}
	if send == nil {
		send = DefaultBlurterSend
	}
	severity = strings.TrimSpace(severity)
	if severity == "" {
		severity = "problem"
	}
	return send(subject, body, severity, key)
}
