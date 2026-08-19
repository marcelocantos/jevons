// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpup

import (
	"context"
	"fmt"
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
	// Upstreams remembers real remote URLs after EnsureMCP rewrites
	// provider configs to loopback. Nil skips resolve (tests with
	// fresh remote URLs only).
	Upstreams *UpstreamRegistry
	// Client / OpenURL / Probe / Authorize / Refresh are passed to
	// Claudia. Production leaves them nil; hermetic tests inject stubs
	// so a fixture 401+refresh never opens a browser (🎯T520).
	Client    *http.Client
	OpenURL   func(string) error
	Probe     func(ctx context.Context, rawURL string) (*claudia.MCPProbe, error)
	Authorize func(ctx context.Context, args *claudia.AuthorizeMCPArgs) (*claudia.MCPToken, error)
	Refresh   func(ctx context.Context, args *claudia.RefreshMCPArgs) (*claudia.MCPToken, error)
	// Ensure writes one advertised loopback registration. Nil uses
	// claudia.EnsureMCP. Isolates pass fixture paths via EnsureArgs.
	Ensure     func(args *claudia.EnsureMCPArgs) error
	EnsureArgs *claudia.EnsureMCPArgs // path overrides only; Name/URL filled per server
}

// Host is the mounted proxy plus its token store.
type Host struct {
	Proxy *claudia.MCPProxy
	Store *Store
}

// Mount builds a Claudia MCPProxy for HTTP owner-map servers, reseeds
// stored tokens, registers Prefix on mux, and EnsureMCPs advertised
// loopback URLs so Session backends never dial upstream directly (🎯T520).
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
	mux.Handle(Prefix+"/", proxy)

	ensure := args.Ensure
	if ensure == nil {
		ensure = claudia.EnsureMCP
	}
	for _, adv := range proxy.Advertised() {
		ea := claudia.EnsureMCPArgs{Name: adv.Name, URL: adv.URL}
		if args.EnsureArgs != nil {
			ea.ClaudeJSON = args.EnsureArgs.ClaudeJSON
			ea.GrokTOML = args.EnsureArgs.GrokTOML
			ea.CodexTOML = args.EnsureArgs.CodexTOML
			ea.Providers = args.EnsureArgs.Providers
		}
		if err := ensure(&ea); err != nil {
			slog.Warn("could not ensure proxied MCP loopback URL",
				"name", adv.Name, "url", adv.URL, "err", err)
			continue
		}
		slog.Info("HTTP MCP proxied via loopback", "name", adv.Name, "url", adv.URL)
	}
	return h, nil
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
