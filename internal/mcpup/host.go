// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/marcelocantos/claudia"
)

const (
	// Prefix is the mux path under which proxied HTTP MCP servers are
	// mounted. Public URL = PublicBase + Prefix + "/" + name.
	Prefix = "/upstream"
)

// MountArgs configures [Mount].
type MountArgs struct {
	// PublicBase is e.g. "http://127.0.0.1:13705".
	PublicBase string
	// Servers is the owner inventory; stdio and empty-URL entries are
	// skipped. SkipNames are also omitted (typically jevonsmcp).
	Servers   []claudia.MCPServer
	SkipNames map[string]bool
	// Store holds durable tokens. Nil means memory-only (tests).
	Store *Store
	// Upstreams remembers real remote URLs when a leftover HOME
	// inventory still lists a loopback from an older EnsureMCP write.
	// Nil skips resolve (tests with fresh remote URLs only).
	Upstreams *UpstreamRegistry
	// Client / OpenURL / Probe / Authorize / Refresh are passed to
	// Claudia. Production leaves them nil; hermetic tests inject stubs
	// so a fixture 401+refresh never opens a browser (🎯T520).
	Client    *http.Client
	OpenURL   func(string) error
	Probe     func(ctx context.Context, rawURL string) (*claudia.MCPProbe, error)
	Authorize func(ctx context.Context, args *claudia.AuthorizeMCPArgs) (*claudia.MCPToken, error)
	Refresh   func(ctx context.Context, args *claudia.RefreshMCPArgs) (*claudia.MCPToken, error)
	// OnToolsCall sees proxied JSON-RPC tools/call names (🎯T64.2).
	OnToolsCall func(name string, args map[string]any)
}

// Host is the mounted proxy plus its token store.
type Host struct {
	Proxy *claudia.MCPProxy
	Store *Store
}

// Mount builds a Claudia MCPProxy for HTTP owner-map servers, reseeds
// stored tokens, and registers Prefix on mux. Advertised() is the
// loopback list SessionServers stamps onto AgentDef.MCPServers (🎯T520).
func Mount(mux *http.ServeMux, args *MountArgs) (*Host, error) {
	if mux == nil || args == nil {
		return nil, fmt.Errorf("mcpup: mux and args required")
	}
	if strings.TrimSpace(args.PublicBase) == "" {
		return nil, fmt.Errorf("mcpup: PublicBase required")
	}
	publicBase := strings.TrimRight(args.PublicBase, "/")
	httpServers := filterHTTP(args.Servers, args.SkipNames)
	if args.Upstreams != nil {
		resolved, err := args.Upstreams.Resolve(httpServers, publicBase+Prefix)
		if err != nil {
			slog.Warn("mcp upstream registry write failed", "err", err)
		}
		httpServers = resolved
	}
	proxyArgs := &claudia.MCPProxyArgs{
		Prefix:     Prefix,
		PublicBase: publicBase,
		Servers:    httpServers,
		Client:     args.Client,
		OpenURL:    args.OpenURL,
		Probe:      args.Probe,
		Authorize:  args.Authorize,
		Refresh:    args.Refresh,
	}
	if args.Store != nil {
		store := args.Store
		proxyArgs.OnTokenChange = func(name string, tok *claudia.MCPToken) {
			if err := store.Put(name, tok); err != nil {
				slog.Warn("mcp oauth token persist failed", "server", name, "err", err)
			}
		}
	}
	proxy, err := claudia.NewMCPProxy(proxyArgs)
	if err != nil {
		return nil, err
	}
	h := &Host{Proxy: proxy, Store: args.Store}
	if args.Store != nil {
		for _, s := range httpServers {
			if tok := args.Store.Get(s.Name); tok != nil {
				if err := proxy.SetToken(s.Name, tok); err != nil {
					slog.Warn("mcp oauth token reseed failed", "server", s.Name, "err", err)
				}
			}
		}
	}
	var handler http.Handler = proxy
	if args.OnToolsCall != nil {
		handler = toolsCallObserver(proxy, Prefix, args.OnToolsCall)
	}
	mux.Handle(Prefix+"/", handler)
	for _, adv := range proxy.Advertised() {
		slog.Info("HTTP MCP proxied via loopback", "name", adv.Name, "url", adv.URL)
	}
	return h, nil
}

// Advertised is the loopback name+URL list seats should carry (🎯T520).
func (h *Host) Advertised() []claudia.MCPServer {
	if h == nil || h.Proxy == nil {
		return nil
	}
	return h.Proxy.Advertised()
}

func toolsCallObserver(next http.Handler, prefix string, observe func(name string, args map[string]any)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && observe != nil {
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err == nil {
				r.Body = io.NopCloser(bytes.NewReader(body))
				if tool, args := parseToolsCall(body); tool != "" {
					observe(stampLabel(upstreamServerName(r.URL.Path, prefix), tool), args)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func parseToolsCall(body []byte) (name string, args map[string]any) {
	var req struct {
		Method string `json:"method"`
		Params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &req) != nil || req.Method != "tools/call" {
		return "", nil
	}
	name = strings.TrimSpace(req.Params.Name)
	if len(req.Params.Arguments) > 0 && string(req.Params.Arguments) != "null" {
		var m map[string]any
		if json.Unmarshal(req.Params.Arguments, &m) == nil && len(m) > 0 {
			args = m
		}
	}
	return name, args
}

func upstreamServerName(path, prefix string) string {
	path = strings.TrimPrefix(path, prefix)
	path = strings.TrimPrefix(path, "/")
	name, _, _ := strings.Cut(path, "/")
	if name == "" || strings.Contains(name, "..") {
		return ""
	}
	return name
}

func stampLabel(server, tool string) string {
	tool = strings.TrimSpace(tool)
	if server == "" {
		return tool
	}
	if strings.Contains(strings.ToLower(tool), strings.ToLower(server)) {
		return tool
	}
	return server + ": " + tool
}

func filterHTTP(servers []claudia.MCPServer, skip map[string]bool) []claudia.MCPServer {
	var out []claudia.MCPServer
	for _, s := range servers {
		if s.Name == "" || strings.TrimSpace(s.URL) == "" {
			continue
		}
		if skip != nil && skip[s.Name] {
			continue
		}
		out = append(out, s)
	}
	return out
}

// PublicBase builds http://host:port for the served listener.
func PublicBase(host string, port int) string {
	return fmt.Sprintf("http://%s:%d", host, port)
}
