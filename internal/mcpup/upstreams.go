// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/marcelocantos/claudia"
)

// UpstreamRegistry remembers the real remote URL for each proxied
// server. EnsureMCP rewrites provider configs to loopback, so the next
// LoadMCP would otherwise feed the proxy its own PublicURL (🎯T520).
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
		if isOurLoopback(url, prefix, s.Name) {
			real, ok := r.byName[s.Name]
			if !ok || real == "" {
				continue // unknown loopback — drop rather than proxy-to-self
			}
			s.URL = real
			out = append(out, s)
			continue
		}
		if r.byName[s.Name] != url {
			r.byName[s.Name] = url
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

func isOurLoopback(url, publicPrefix, name string) bool {
	want := publicPrefix + "/" + name
	return url == want || strings.HasPrefix(url, want+"/")
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
