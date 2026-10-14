// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpup

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/marcelocantos/claudia"
)

// UpstreamRegistry remembers the real remote URL for each proxied
// server. A leftover HOME inventory may still list a loopback from an
// older EnsureMCP write; Resolve puts the remote back so the proxy
// does not dial itself (🎯T520).
type UpstreamRegistry struct {
	path string

	mu     sync.Mutex
	byName map[string]string
}

// OpenUpstreamRegistry loads path (missing → empty).
func OpenUpstreamRegistry(path string) (*UpstreamRegistry, error) {
	r := &UpstreamRegistry{path: path, byName: map[string]string{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return nil, fmt.Errorf("mcpup upstreams: read: %w", err)
	}
	if len(b) == 0 {
		return r, nil
	}
	var doc map[string]string
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("mcpup upstreams: parse %s: %w", path, err)
	}
	for k, v := range doc {
		r.byName[k] = v
	}
	return r, nil
}

// Resolve returns proxy-ready servers: remote URLs are remembered;
// already-advertised loopback URLs are replaced from the registry.
//
// A nested /upstream/ leftover — on this daemon's prefix or any other
// loopback port — is never an upstream (🎯T1035 / 🎯T1039). Remembering
// one poisoned mcp_upstreams.json (a dead isolate at :52322 overwrote
// the owner's direct grant) and then every seat dialled the nested
// proxy instead of the real server.
func (r *UpstreamRegistry) Resolve(servers []claudia.MCPServer, publicPrefix string) ([]claudia.MCPServer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prefix := strings.TrimRight(publicPrefix, "/")
	changed := false
	var out []claudia.MCPServer
	for _, s := range servers {
		raw := strings.TrimSpace(s.URL)
		if raw == "" || s.Name == "" {
			continue
		}
		if isLeftoverProxyURL(raw, prefix) {
			real, ok := r.byName[s.Name]
			if ok && isLeftoverProxyURL(real, prefix) {
				slog.Warn("mcp upstream registry named a nested proxy as the upstream; dropped so a leftover cannot be dialled (🎯T1039)",
					"name", s.Name, "url", real)
				delete(r.byName, s.Name)
				changed = true
				real = ""
			}
			if real == "" {
				continue // unknown leftover — drop rather than remember or proxy-to-self
			}
			s.URL = real
			out = append(out, s)
			continue
		}
		if r.byName[s.Name] != raw {
			r.byName[s.Name] = raw
			changed = true
		}
		out = append(out, s)
	}
	if changed {
		if err := r.flushLocked(); err != nil {
			return out, err
		}
	}
	return out, nil
}

// isLeftoverProxyURL reports a URL that is this proxy (🎯T1035) or any
// loopback nested /upstream/ leftover from another isolate (🎯T1039).
func isLeftoverProxyURL(raw, prefix string) bool {
	return IsNestedProxyURL(raw) || routesToPrefix(raw, prefix)
}

// IsNestedProxyURL reports a loopback URL whose path is /upstream or
// /upstream/<name> — the shape jevonsd (and isolate daemons) advertise
// as a proxy, on any port. A leftover on :52322 is the same class of
// poison as a leftover on this daemon's :13705.
func IsNestedProxyURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return isNestedProxyURL(u)
}

func isNestedProxyURL(u *url.URL) bool {
	if u == nil || !isLoopbackHost(u.Hostname()) {
		return false
	}
	path := u.EscapedPath()
	if path == "" {
		path = u.Path
	}
	return path == Prefix || strings.HasPrefix(path, Prefix+"/")
}

// GrantDirect reports whether seats should dial raw itself rather than
// a jevonsd /upstream/ loopback. Local MCP (bullseye, mnemo, …) is
// already on loopback and is not OAuth; stamping the proxy URL is what
// taught Claudia to persist :52322/upstream/bullseye (🎯T1039). Remote
// HTTP (Atlassian) still takes the T520 proxy.
func GrantDirect(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.TrimSpace(u.Host) == "" {
		return false
	}
	if !isLoopbackHost(u.Hostname()) {
		return false
	}
	return !isNestedProxyURL(u)
}

// routesToPrefix reports whether rawURL reaches the proxy mounted at prefix
// (e.g. "http://127.0.0.1:13705/upstream"): same scheme and port, a loopback
// host however it is spelt (127.0.0.1, localhost, [::1]), and a path under
// the prefix for any server name.
func routesToPrefix(rawURL, prefix string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	p, err := url.Parse(prefix)
	if err != nil {
		return false
	}
	if !strings.EqualFold(u.Scheme, p.Scheme) || u.Port() != p.Port() {
		return false
	}
	if !isLoopbackHost(u.Hostname()) || !isLoopbackHost(p.Hostname()) {
		return false
	}
	return strings.HasPrefix(u.Path, strings.TrimRight(p.Path, "/")+"/")
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (r *UpstreamRegistry) flushLocked() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r.byName, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

// UpstreamRegistryPath is state_dir/mcp_upstreams.json.
func UpstreamRegistryPath(stateDir string) string {
	return filepath.Join(stateDir, "mcp_upstreams.json")
}
