// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package portown

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Probe is one GET of /api/agents.
type Probe struct {
	OK          bool
	Fingerprint string
	Err         string
}

// Fleet is the pair the owner URL and the loopback bind must agree on.
type Fleet struct {
	Loopback  Probe // 127.0.0.1
	Localhost Probe
}

// Mismatch is true when the two probes do not show the same fleet.
// Both failing is 🎯T405 (nothing serving), not this alarm.
func (f Fleet) Mismatch() bool {
	if !f.Loopback.OK && !f.Localhost.OK {
		return false
	}
	if f.Loopback.OK != f.Localhost.OK {
		return true
	}
	return f.Loopback.Fingerprint != f.Localhost.Fingerprint
}

// Text names the mismatch. Empty when the fleets agree or both failed.
func (f Fleet) Text() string {
	if !f.Mismatch() {
		return ""
	}
	return fmt.Sprintf("localhost and 127.0.0.1 disagree on /api/agents: localhost=%s 127.0.0.1=%s",
		probeBrief(f.Localhost), probeBrief(f.Loopback))
}

func probeBrief(p Probe) string {
	if !p.OK {
		if p.Err != "" {
			return "error:" + p.Err
		}
		return "unreachable"
	}
	if p.Fingerprint == "" {
		return "empty"
	}
	return p.Fingerprint
}

// AlarmText is the owner-facing sentence, or empty when quiet.
// Listeners and fleet mismatch compose; either is enough to shout.
func AlarmText(c Conflict, fleet Fleet) string {
	var parts []string
	if t := c.Text(); t != "" {
		parts = append(parts, t)
	}
	if t := fleet.Text(); t != "" {
		parts = append(parts, t)
	}
	return strings.Join(parts, " ")
}

// FingerprintAgents turns a /api/agents body into a stable name list.
// A JSON array of objects with "name" is the product shape. Anything
// else hashes so two identical error bodies still compare equal.
func FingerprintAgents(body []byte) string {
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		sum := sha256.Sum256(body)
		return fmt.Sprintf("body:%x", sum[:8])
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		n, _ := r["name"].(string)
		n = strings.TrimSpace(n)
		if n != "" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// ProbeAgents GETs /api/agents on 127.0.0.1 and on localhost.
func ProbeAgents(port int) Fleet {
	return ProbeAgentsWith(defaultClient(), port)
}

// ProbeAgentsWith is the test seam.
func ProbeAgentsWith(client *http.Client, port int) Fleet {
	if client == nil {
		client = defaultClient()
	}
	return Fleet{
		Loopback:  getAgents(client, fmt.Sprintf("http://127.0.0.1:%d/api/agents", port)),
		Localhost: getAgents(client, fmt.Sprintf("http://localhost:%d/api/agents", port)),
	}
}

func defaultClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func getAgents(client *http.Client, url string) Probe {
	resp, err := client.Get(url)
	if err != nil {
		return Probe{Err: err.Error()}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Probe{Err: err.Error()}
	}
	if resp.StatusCode != http.StatusOK {
		return Probe{Err: fmt.Sprintf("HTTP %d", resp.StatusCode), Fingerprint: FingerprintAgents(body)}
	}
	return Probe{OK: true, Fingerprint: FingerprintAgents(body)}
}
