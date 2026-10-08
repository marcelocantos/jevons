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
// A route back to the proxy is never an upstream (🎯T1035): a URL that
// reaches publicPrefix — under any loopback spelling or server name — is
// not remembered, and a remembered "real" URL that is itself such a route
// is deleted rather than proxied. Proxying one meant every request
// recursed through the proxy's own listener until the host ran out of
// ephemeral ports.
func (r *UpstreamRegistry) Resolve(servers []claudia.MCPServer, publicPrefix string) ([]claudia.MCPServer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prefix := strings.TrimRight(publicPrefix, "/")
	changed := false
	var out []claudia.MCPServer
	for _, s := range servers {
		url := strings.TrimSpace(s.URL)
		if url == "" || s.Name == "" {
			continue
		}
		if !routesToPrefix(url, prefix) {
			if r.byName[s.Name] != url {
				r.byName[s.Name] = url
				changed = true
			}
			out = append(out, s)
			continue
		}
		real, ok := r.byName[s.Name]
		if ok && routesToPrefix(real, prefix) {
			slog.Warn("mcp upstream registry named the proxy itself as the upstream; dropped so the proxy cannot dial itself (🎯T1035)",
				"name", s.Name, "url", real)
			delete(r.byName, s.Name)
			changed = true
			real = ""
		}
		if real == "" {
			continue // unknown loopback — drop rather than proxy-to-self
		}
		s.URL = real
		out = append(out, s)
	}
	if changed {
		if err := r.flushLocked(); err != nil {
			return out, err
		}
	}
	return out, nil
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
