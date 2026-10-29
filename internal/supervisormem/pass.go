// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervisormem

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Pass wire keywords (🎯T700). Tests and docratchets pin these prefixes so a
// muzzled pass cannot masquerade as a healthy quiet one.
const (
	PassLineQuietHealthy = "quiet: nothing wake-worthy"
	PassPrefixEscalated  = "escalated:"
	PassPrefixMuzzled    = "muzzled:"
)

// WakeFixture is the hermetic oracle input for one unattended pass tick.
type WakeFixture struct {
	WakeWorthy bool   `json:"wake_worthy"`
	Subject    string `json:"subject"`
	Body       string `json:"body"`
	Severity   string `json:"severity"`
	Key        string `json:"key"`
}

// PassOutcome is what the pass reports after attempting escalation.
type PassOutcome struct {
	Line       string
	Escalated  bool
	Muzzled    bool
	Spool      string
	EscalError string
}

// FinishPass decides the terminal line for an unattended supervision tick.
// A wake-worthy finding that cannot reach blurter is muzzled, never quiet.
func FinishPass(wake bool, spool string, escalateErr error) PassOutcome {
	if !wake {
		return PassOutcome{Line: PassLineQuietHealthy}
	}
	if escalateErr == nil {
		line := PassPrefixEscalated + " owner notified via daemon blurter"
		if s := strings.TrimSpace(spool); s != "" {
			line += " spool=" + s
		}
		return PassOutcome{Line: line, Escalated: true, Spool: spool}
	}
	return PassOutcome{
		Line:       PassPrefixMuzzled + " escalation refused: " + strings.TrimSpace(escalateErr.Error()),
		Muzzled:    true,
		EscalError: escalateErr.Error(),
	}
}

// EscalateViaHTTP posts to POST /api/supervisor/escalate on the development
// daemon (or an httptest server). The caller need not run blurter through Bash
// or seat MCP blurter tools (🎯T700 / 🎯T704).
func EscalateViaHTTP(baseURL, subject, body, severity, key string, client *http.Client) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("daemon base URL required")
	}
	payload, err := json.Marshal(map[string]string{
		"subject":  strings.TrimSpace(subject),
		"body":     strings.TrimSpace(body),
		"severity": strings.TrimSpace(severity),
		"key":      strings.TrimSpace(key),
	})
	if err != nil {
		return "", err
	}
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/supervisor/escalate", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(bodyBytes))
		if msg == "" {
			msg = resp.Status
		}
		return "", fmt.Errorf("escalate HTTP %d: %s", resp.StatusCode, msg)
	}
	var out struct {
		Spool string `json:"spool"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		return "", fmt.Errorf("decode escalate response: %w", err)
	}
	return strings.TrimSpace(out.Spool), nil
}

// RunUnattendedPass exercises the wake-worthy escalation path against a live
// or test daemon URL. It returns the pass footer line and outcome metadata.
func RunUnattendedPass(baseURL string, fix WakeFixture, client *http.Client) (PassOutcome, error) {
	if !fix.WakeWorthy {
		return FinishPass(false, "", nil), nil
	}
	subject := strings.TrimSpace(fix.Subject)
	body := strings.TrimSpace(fix.Body)
	if subject == "" || body == "" {
		return PassOutcome{}, fmt.Errorf("wake-worthy fixture needs subject and body")
	}
	spool, err := EscalateViaHTTP(baseURL, subject, body, fix.Severity, fix.Key, client)
	return FinishPass(true, spool, err), nil
}

// DefaultWakeFixtureSubject is the stable subject used by the T700 hermetic oracle.
const DefaultWakeFixtureSubject = "jevons-supervisor T700 fixture"

// DefaultWakeFixture returns a wake-worthy fixture with a timestamped body.
func DefaultWakeFixture(now time.Time) WakeFixture {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return WakeFixture{
		WakeWorthy: true,
		Subject:    DefaultWakeFixtureSubject,
		Body:       fmt.Sprintf("hermetic unattended pass %s — expiring capacity nobody is using (fixture)", now.Format(time.RFC3339)),
		Severity:   "warning",
		Key:        "t700-hermetic",
	}
}
