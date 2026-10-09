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

	// HopHeader marks a request the proxy sends upstream. A request that
	// arrives carrying it has already been through a jevons upstream proxy,
	// so it is refused rather than forwarded again (🎯T1035): a route that
	// leads back to a proxy costs one refused hop, not a recursion that
	// opens a connection per level until the host's ephemeral ports run out.
	HopHeader = "X-Jevons-Upstream-Hop"
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
	// resolved is the real upstream URL for each mounted server after
	// Resolve (owner grant, not a nested leftover). Advertised rewrites
	// loopback grants onto this list (🎯T1039).
	resolved []claudia.MCPServer
}

// Mount builds a Claudia MCPProxy for HTTP owner-map servers, reseeds
// stored tokens, and registers Prefix on mux. Advertised() is the
// grant list SessionServers stamps onto AgentDef.MCPServers: T520
// loopback proxy URLs for remote OAuth, the owner's direct URL for
// local loopback MCP (🎯T1039).
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
	httpServers = dropSelfRoutes(httpServers, publicBase+Prefix)
	client := &http.Client{}
	if args.Client != nil {
		*client = *args.Client
	}
	next := client.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	client.Transport = hopTransport{next: next}
	proxyArgs := &claudia.MCPProxyArgs{
		Prefix:     Prefix,
		PublicBase: publicBase,
		Servers:    httpServers,
		Client:     client,
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
	h := &Host{Proxy: proxy, Store: args.Store, resolved: httpServers}
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
	mux.Handle(Prefix+"/", refuseHops(handler))
	for _, adv := range h.Advertised() {
		slog.Info("HTTP MCP granted on AgentDef.MCPServers", "name", adv.Name, "url", adv.URL)
	}
	return h, nil
}

// Advertised is the name+URL list seats should carry. Remote OAuth
// servers keep the T520 jevonsd /upstream/ loopback; a resolved
// loopback MCP is granted its direct URL so Claudia does not persist a
// nested leftover (🎯T1039).
func (h *Host) Advertised() []claudia.MCPServer {
	if h == nil || h.Proxy == nil {
		return nil
	}
	adv := h.Proxy.Advertised()
	if len(h.resolved) == 0 {
		return adv
	}
	realByName := make(map[string]string, len(h.resolved))
	for _, s := range h.resolved {
		if s.Name == "" || strings.TrimSpace(s.URL) == "" {
			continue
		}
		realByName[s.Name] = s.URL
	}
	out := append([]claudia.MCPServer(nil), adv...)
	for i, s := range out {
		real := realByName[s.Name]
		if !GrantDirect(real) {
			continue
		}
		out[i].URL = real
		if out[i].Type == "" {
			out[i].Type = "http"
		}
	}
	return out
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

// hopTransport stamps HopHeader on every request the proxy sends.
type hopTransport struct{ next http.RoundTripper }

func (t hopTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set(HopHeader, "1")
	return t.next.RoundTrip(req)
}

// refuseHops answers 508 Loop Detected to a request that already passed
// through a jevons upstream proxy, instead of forwarding it again.
func refuseHops(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(HopHeader) != "" {
			slog.Warn("mcp upstream proxy loop refused: request already came through a jevons upstream proxy (🎯T1035)",
				"path", r.URL.Path)
			http.Error(w, "mcp upstream proxy loop: this server's upstream leads back to a jevons upstream proxy", http.StatusLoopDetected)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// dropSelfRoutes removes servers whose URL leads back to this proxy, which
// no registry or inventory can make a real upstream (🎯T1035).
func dropSelfRoutes(servers []claudia.MCPServer, prefix string) []claudia.MCPServer {
	var out []claudia.MCPServer
	for _, s := range servers {
		if routesToPrefix(s.URL, prefix) {
			slog.Warn("mcp upstream not proxied: its URL is this proxy (🎯T1035)", "name", s.Name, "url", s.URL)
			continue
		}
		out = append(out, s)
	}
	return out
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
