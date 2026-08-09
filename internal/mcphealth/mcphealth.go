// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package mcphealth detects MCP registrations that can never resolve
// (🎯T379).
//
// An agent inherits its MCP server list from the provider's user-scoped
// config. When one of those entries points at a URL nothing listens on, the
// client does not fail — it sits in "still connecting" forever and the agent
// simply runs without those tools. Nothing in the product notices, so the
// only symptom is work that quietly lacks a capability it was supposed to
// have. (Found live: `jevonsmcp-journey` registered at 127.0.0.1:13715, a
// journey-suite port whose teardown never ran; jevonsd serves 13705.)
//
// The distinction that makes this detectable is at the TCP layer, not the
// MCP layer: a *refused* connection proves nothing is bound to that port,
// while a *timeout* is indistinguishable from a peer that is merely slow.
// Classifying those two differently is what lets the caller say "dead" about
// the first without libelling the second.
package mcphealth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// DefaultTimeout bounds a single probe. Probes run concurrently and only
// establish a TCP connection, so this is the worst-case added latency for a
// whole sweep, not per registration.
const DefaultTimeout = 700 * time.Millisecond

// Status is the reachability verdict for one registration.
type Status string

const (
	// StatusLive means a TCP connection was established.
	StatusLive Status = "live"
	// StatusDead means the endpoint provably has no listener: the
	// connection was refused, or the host does not resolve. Retrying does
	// not help; the registration is wrong.
	StatusDead Status = "dead"
	// StatusSlow means the dial timed out. Nothing is proven — the peer may
	// be starting up, or filtered. Explicitly not dead.
	StatusSlow Status = "slow"
	// StatusSkipped means the entry is not probeable over TCP (stdio
	// transport: the client spawns a command, there is no endpoint).
	StatusSkipped Status = "skipped"
	// StatusUnknown means the dial failed in a way that proves nothing, or
	// the URL could not be parsed into an address.
	StatusUnknown Status = "unknown"
)

// Registration is one MCP server entry from a provider's config.
type Registration struct {
	Name string
	// Transport is the config's declared type: http, sse, or stdio.
	Transport string
	// URL is the endpoint for http/sse transports; empty for stdio.
	URL string
}

// Result is a Registration plus its probe verdict.
type Result struct {
	Registration
	Status Status
	// Detail explains the verdict in one clause, for a human reading a log
	// line or an agent-start note.
	Detail string
}

// Dead reports whether this registration provably cannot resolve.
func (r Result) Dead() bool { return r.Status == StatusDead }

// ClassifyDialErr maps the outcome of a TCP dial to a Status.
//
// This is the pure core of acceptance 2. The three cases that matter:
//
//	nil                → live
//	ECONNREFUSED / NXDOMAIN → dead (nothing is listening, and won't be)
//	timeout            → slow (may merely be starting up — never "dead")
//
// Anything else proves nothing and returns StatusUnknown rather than
// guessing, so a novel network error cannot be laundered into a false
// "dead" report.
func ClassifyDialErr(err error) (Status, string) {
	if err == nil {
		return StatusLive, "connected"
	}
	// Timeout first: a DNS lookup that times out is slow, not missing.
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return StatusSlow, "dial timed out — peer may be slow to start or filtered"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return StatusDead, "host does not resolve"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return StatusDead, "connection refused — nothing is listening on that port"
	}
	return StatusUnknown, err.Error()
}

// DialAddress derives the host:port to probe from an MCP endpoint URL.
// A missing port is filled from the scheme, because a registration that
// omits it still resolves to a concrete port at connect time.
func DialAddress(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse %q: %w", raw, err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("no host in %q", raw)
	}
	if u.Port() != "" {
		return u.Host, nil
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "ws":
		return net.JoinHostPort(u.Hostname(), "80"), nil
	case "https", "wss":
		return net.JoinHostPort(u.Hostname(), "443"), nil
	default:
		return "", fmt.Errorf("no port and unknown scheme %q in %q", u.Scheme, raw)
	}
}

// Probeable reports whether a registration has an endpoint worth dialling.
// stdio entries do not: the client spawns a process, so there is no address
// that can be dead in this sense.
func Probeable(reg Registration) bool {
	switch strings.ToLower(strings.TrimSpace(reg.Transport)) {
	case "http", "sse", "streamable-http", "":
		return strings.TrimSpace(reg.URL) != ""
	default:
		return false
	}
}

// dialFunc is the seam tests use to force a classification without needing a
// real network condition for every case.
type dialFunc func(network, addr string, timeout time.Duration) (net.Conn, error)

// Probe classifies one registration. It opens and immediately closes a TCP
// connection; it does not speak MCP, because reachability is the question
// and a protocol handshake would make a slow-but-real server look broken.
func Probe(reg Registration, timeout time.Duration) Result {
	return probeWith(reg, timeout, net.DialTimeout)
}

func probeWith(reg Registration, timeout time.Duration, dial dialFunc) Result {
	res := Result{Registration: reg}
	if !Probeable(reg) {
		res.Status = StatusSkipped
		res.Detail = "not an http endpoint"
		return res
	}
	addr, err := DialAddress(reg.URL)
	if err != nil {
		res.Status = StatusUnknown
		res.Detail = err.Error()
		return res
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	conn, dialErr := dial("tcp", addr, timeout)
	if conn != nil {
		_ = conn.Close()
	}
	res.Status, res.Detail = ClassifyDialErr(dialErr)
	return res
}

// ProbeAll probes every registration concurrently and returns results in the
// input order. One unreachable entry never delays the others.
func ProbeAll(regs []Registration, timeout time.Duration) []Result {
	out := make([]Result, len(regs))
	var wg sync.WaitGroup
	for i, reg := range regs {
		wg.Add(1)
		go func(i int, reg Registration) {
			defer wg.Done()
			out[i] = Probe(reg, timeout)
		}(i, reg)
	}
	wg.Wait()
	return out
}

// Dead returns only the results that provably cannot resolve.
func Dead(results []Result) []Result {
	var out []Result
	for _, r := range results {
		if r.Dead() {
			out = append(out, r)
		}
	}
	return out
}

// Report renders a one-line warning naming every dead registration, or ""
// when none is dead. Live, slow, and skipped entries are never mentioned:
// the note exists to surface a fault, and listing healthy servers would
// train the reader to skim past it.
func Report(results []Result) string {
	dead := Dead(results)
	if len(dead) == 0 {
		return ""
	}
	parts := make([]string, 0, len(dead))
	for _, r := range dead {
		parts = append(parts, fmt.Sprintf("%s (%s: %s)", r.Name, r.URL, r.Detail))
	}
	noun := "registration"
	if len(dead) > 1 {
		noun = "registrations"
	}
	return fmt.Sprintf("dead MCP %s — tools from %s will never attach: %s",
		noun, plural(len(dead), "this server", "these servers"), strings.Join(parts, "; "))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// claudeConfig is the subset of ~/.claude.json this package reads: the
// user-scoped server map every Claude agent inherits, regardless of project.
type claudeConfig struct {
	MCPServers map[string]struct {
		Type    string `json:"type"`
		URL     string `json:"url"`
		Command string `json:"command"`
	} `json:"mcpServers"`
}

// ParseClaudeRegistrations reads the user-scope `mcpServers` map out of a
// Claude Code config document. Project-scoped blocks are deliberately
// ignored: an entry there affects one workdir, whereas a user-scope entry is
// carried by every agent the fleet starts — which is the failure this
// package exists to catch.
//
// Registrations come back sorted by name so a report is stable across runs.
func ParseClaudeRegistrations(data []byte) ([]Registration, error) {
	var cfg claudeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse claude config: %w", err)
	}
	out := make([]Registration, 0, len(cfg.MCPServers))
	for name, e := range cfg.MCPServers {
		transport := strings.TrimSpace(e.Type)
		if transport == "" && strings.TrimSpace(e.Command) != "" {
			transport = "stdio"
		}
		out = append(out, Registration{
			Name:      name,
			Transport: transport,
			URL:       strings.TrimSpace(e.URL),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
