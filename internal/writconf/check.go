// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package writconf

import (
	"net/url"
	"path/filepath"
	"strings"
)

// EventKind classifies confinement / auditor observations (🎯T335).
type EventKind string

const (
	// KindDenyNet is an undeclared network host/path refused by egress.
	KindDenyNet EventKind = "deny_net"
	// KindDenyFS is an undeclared filesystem access refused by GateFS/seatbelt.
	KindDenyFS EventKind = "deny_fs"
	// KindDrift is declared-vs-actual mismatch (eslogger / offline audit).
	KindDrift EventKind = "drift"
	// KindBypass is a retry or execution outside the sandbox class after deny.
	KindBypass EventKind = "bypass"
	// KindSecretPath is access toward credential / secret-bearing paths.
	KindSecretPath EventKind = "secret_path"
	// KindHighRiskEgress is traffic toward high-risk exfil patterns.
	KindHighRiskEgress EventKind = "high_risk_egress"
	// KindPolicyDeny is a preflight policy deny (doit/jwork) surfaced to auditor.
	KindPolicyDeny EventKind = "policy_deny"
)

// Event is one confinement or security observation for the auditor.
type Event struct {
	Kind          EventKind `json:"kind"`
	Agent         string    `json:"agent,omitempty"`
	Host          string    `json:"host,omitempty"`
	Path          string    `json:"path,omitempty"`
	ManifestField string    `json:"manifest_field,omitempty"`
	Message       string    `json:"message,omitempty"`
	// OutsideSandbox is true when the attempt was not under writ (bypass class).
	OutsideSandbox bool `json:"outside_sandbox,omitempty"`
}

// NetAllowed reports whether host is covered by a host-granularity or fetch intent.
// Pure mirror of writ egress allowlist semantics for hermetic tests.
func NetAllowed(m *Manifest, host string) bool {
	if m == nil {
		return false
	}
	host = normalizeHost(host)
	if host == "" {
		return false
	}
	for _, n := range m.Net {
		if h := normalizeHost(n.Host); h != "" && (h == host || strings.HasSuffix(host, "."+h)) {
			return true
		}
		if n.Fetch != "" {
			if fh := hostFromFetch(n.Fetch); fh != "" && (fh == host || strings.HasSuffix(host, "."+fh)) {
				return true
			}
		}
	}
	return false
}

// PathDeclared reports whether path is under a declared fs read/edit glob prefix.
// Hermetic approximation: prefix / filepath.Match against globs (no full GateFS).
func PathDeclared(m *Manifest, path, mode string) bool {
	if m == nil || m.FS == nil {
		return false
	}
	path = filepath.Clean(path)
	mode = strings.ToLower(strings.TrimSpace(mode))
	check := func(targets []FSTarget) bool {
		for _, t := range targets {
			if globMatch(t.Glob, path) {
				return true
			}
		}
		return false
	}
	switch mode {
	case "edit", "write":
		return check(m.FS.Edit)
	default:
		return check(m.FS.Read) || check(m.FS.Edit)
	}
}

// IsSecretPath reports paths that must never be casually exfiltrated.
func IsSecretPath(path string) bool {
	p := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	if p == "" {
		return false
	}
	// Home-relative and absolute credential trees.
	needles := []string{
		"/.ssh/", "/.ssh",
		"/.aws/", "/.aws",
		"/.gnupg/", "/.gnupg",
		"/.config/gcloud/",
		"/.kube/config",
		"/.netrc",
		"/.env",
		"/credentials",
		"/id_rsa",
		"/id_ed25519",
		"/secrets/",
		"/.jevons/secrets",
	}
	base := filepath.Base(p)
	if base == ".env" || base == ".netrc" || strings.HasPrefix(base, "id_rsa") || strings.HasPrefix(base, "id_ed25519") {
		return true
	}
	for _, n := range needles {
		if strings.Contains(p, n) {
			return true
		}
	}
	return false
}

// IsHighRiskHost reports hostnames that warrant standing security interest
// (confused-agent exfil / geopolitical money-or-data risk patterns).
func IsHighRiskHost(host string) bool {
	h := normalizeHost(host)
	if h == "" {
		return false
	}
	// Explicit high-risk TLDs / regions commonly used in owner briefings.
	// This is an interest list for alerts, not a complete geo blocklist.
	suffixes := []string{
		".ru", ".su", ".by", ".cn",
	}
	for _, s := range suffixes {
		if strings.HasSuffix(h, s) {
			return true
		}
	}
	// Known sink / paste / exfil-shaped hosts (illustrative; residual grows).
	bad := []string{
		"pastebin.com",
		"transfer.sh",
		"webhook.site",
		"requestbin.com",
		"ngrok.io",
	}
	for _, b := range bad {
		if h == b || strings.HasSuffix(h, "."+b) {
			return true
		}
	}
	return false
}

// ClassifyAccess pure-classifies a proposed net/fs touch against a manifest.
// Returns nil when the access is allowed and not high-risk/secret.
func ClassifyAccess(m *Manifest, agent, host, path, mode string) *Event {
	host = normalizeHost(host)
	path = strings.TrimSpace(path)
	if path != "" && IsSecretPath(path) {
		return &Event{
			Kind:    KindSecretPath,
			Agent:   agent,
			Path:    path,
			Host:    host,
			Message: "secret-bearing path access",
		}
	}
	if host != "" {
		if IsHighRiskHost(host) {
			return &Event{
				Kind:          KindHighRiskEgress,
				Agent:         agent,
				Host:          host,
				ManifestField: "net[]",
				Message:       "high-risk egress host",
			}
		}
		if !NetAllowed(m, host) {
			return &Event{
				Kind:          KindDenyNet,
				Agent:         agent,
				Host:          host,
				ManifestField: "net[].host|net[].fetch",
				Message:       "undeclared network host",
			}
		}
	}
	if path != "" && m != nil && m.FS != nil && !PathDeclared(m, path, mode) {
		return &Event{
			Kind:          KindDenyFS,
			Agent:         agent,
			Path:          path,
			ManifestField: "fs.read|fs.edit",
			Message:       "undeclared filesystem access",
		}
	}
	return nil
}

// ClassifyBypass marks an execution attempt outside writ after a prior deny.
func ClassifyBypass(agent, detail string) Event {
	return Event{
		Kind:           KindBypass,
		Agent:          agent,
		Message:        detail,
		OutsideSandbox: true,
	}
}

// ClassifyDrift builds a drift event from declared-vs-actual audit.
func ClassifyDrift(agent, path, host, reason string) Event {
	return Event{
		Kind:    KindDrift,
		Agent:   agent,
		Path:    path,
		Host:    host,
		Message: reason,
	}
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	// strip port
	if i := strings.LastIndex(host, ":"); i > 0 && !strings.Contains(host[i:], "]") {
		// avoid mangling IPv6 without brackets; simple hostname:port only
		if !strings.Contains(host, "::") {
			host = host[:i]
		}
	}
	return host
}

func hostFromFetch(fetch string) string {
	fetch = strings.TrimSpace(fetch)
	if fetch == "" {
		return ""
	}
	if !strings.Contains(fetch, "://") {
		fetch = "https://" + fetch
	}
	u, err := url.Parse(fetch)
	if err != nil {
		return ""
	}
	return normalizeHost(u.Hostname())
}

func globMatch(pattern, path string) bool {
	pattern = filepath.Clean(pattern)
	path = filepath.Clean(path)
	if pattern == path {
		return true
	}
	// ** suffix → directory tree prefix
	if strings.HasSuffix(pattern, "**") {
		root := strings.TrimSuffix(pattern, "**")
		root = strings.TrimSuffix(root, string(filepath.Separator))
		root = strings.TrimSuffix(root, "/")
		if root == "" {
			return true
		}
		return path == root || strings.HasPrefix(path, root+string(filepath.Separator)) || strings.HasPrefix(path, root+"/")
	}
	ok, err := filepath.Match(pattern, path)
	if err == nil && ok {
		return true
	}
	// Prefix directory grant (writ expands globs to seatbelt roots).
	if !strings.ContainsAny(pattern, "*?[") {
		return path == pattern || strings.HasPrefix(path, pattern+string(filepath.Separator)) || strings.HasPrefix(path, pattern+"/")
	}
	return false
}
