// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpup

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T1035: the upstream proxy forwarded /upstream/<name> to its own
// listener when the upstream registry named jevonsd's loopback as the
// "real" URL. Each hop opened a fresh connection to itself, so one client
// request recursed until connect() failed with EADDRNOTAVAIL — about
// 16,000 TIME_WAIT sockets to :13705 and every local MCP server down.

// countingListener counts the connections the proxy's own listener accepts.
type countingListener struct {
	net.Listener
	accepted atomic.Int64
}

func (l *countingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.accepted.Add(1)
	}
	return c, err
}

// selfProxyCeiling stands in for the port range the real loop exhausts, so
// a regression fails this test instead of the host.
const selfProxyCeiling = 64

// serveSelfProxy mounts the proxy on a real loopback listener whose public
// base is its own address, with mnemo's upstream set to that same proxy.
func serveSelfProxy(t *testing.T, upstreams *UpstreamRegistry, servers func(base string) []claudia.MCPServer) (base string, accepted func() int64) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cl := &countingListener{Listener: ln}
	base = "http://" + ln.Addr().String()
	mux := http.NewServeMux()
	if _, err := Mount(mux, &MountArgs{PublicBase: base, Servers: servers(base), Upstreams: upstreams}); err != nil {
		t.Fatal(err)
	}
	var inflight atomic.Int64
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if inflight.Add(1) > selfProxyCeiling {
			http.Error(w, "test ceiling: proxy recursed", http.StatusServiceUnavailable)
			inflight.Add(-1)
			return
		}
		defer inflight.Add(-1)
		mux.ServeHTTP(w, r)
	})}
	go srv.Serve(cl)
	t.Cleanup(func() { srv.Close() })
	return base, cl.accepted.Load
}

func postInitialize(t *testing.T, url string) int {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	client := &http.Client{Timeout: 20 * time.Second} // 🎯T97 exemption: bounds a regression's recursion, never the verdict — the verdict is the accept count.
	resp, err := client.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// A server whose URL is the proxy itself is not proxied at all: the
// request costs the client's own connection and nothing more.
func TestT1035ProxyRefusesToForwardToItself(t *testing.T) {
	base, accepted := serveSelfProxy(t, nil, func(base string) []claudia.MCPServer {
		port := base[strings.LastIndex(base, ":")+1:]
		return []claudia.MCPServer{{Name: "mnemo", URL: "http://localhost:" + port + Prefix + "/mnemo"}}
	})
	status := postInitialize(t, base+Prefix+"/mnemo")
	if n := accepted(); n > 1 {
		t.Fatalf("one client request opened %d connections to the proxy's own listener; want 1", n)
	}
	if status != http.StatusNotFound {
		t.Fatalf("status = %d; want 404 (a self route is not mounted)", status)
	}
}

// Two proxies whose upstreams lead to each other — no address check can
// see that loop — stop at the first hop: the second proxy refuses a
// request that already came through one.
func TestT1035ProxiesThatPointAtEachOtherStopAtOneHop(t *testing.T) {
	lnB, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	baseB := "http://" + lnB.Addr().String()
	lnB.Close() // B is served below on this address; A only needs its URL now.
	baseA, acceptedA := serveSelfProxy(t, nil, func(string) []claudia.MCPServer {
		return []claudia.MCPServer{{Name: "mnemo", URL: baseB + Prefix + "/mnemo"}}
	})
	lnB, err = net.Listen("tcp", lnB.Addr().String())
	if err != nil {
		t.Skipf("could not re-bind B's port: %v", err)
	}
	cl := &countingListener{Listener: lnB}
	muxB := http.NewServeMux()
	if _, err := Mount(muxB, &MountArgs{PublicBase: baseB, Servers: []claudia.MCPServer{{Name: "mnemo", URL: baseA + Prefix + "/mnemo"}}}); err != nil {
		t.Fatal(err)
	}
	var inflight atomic.Int64
	srvB := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if inflight.Add(1) > selfProxyCeiling {
			http.Error(w, "test ceiling: proxy recursed", http.StatusServiceUnavailable)
			inflight.Add(-1)
			return
		}
		defer inflight.Add(-1)
		muxB.ServeHTTP(w, r)
	})}
	go srvB.Serve(cl)
	t.Cleanup(func() { srvB.Close() })

	status := postInitialize(t, baseA+Prefix+"/mnemo")
	if a, b := acceptedA(), cl.accepted.Load(); a+b > 2 {
		t.Fatalf("one client request opened %d connections to A and %d to B; want 1 each", a, b)
	}
	if status != http.StatusLoopDetected {
		t.Fatalf("status = %d; want 508 Loop Detected relayed from B", status)
	}
}

// A registry entry that names the proxy's own loopback as the real
// upstream is dropped (and removed from disk), so the proxy has no route
// to itself at all.
func TestT1035PoisonedRegistryIsHealedNotProxied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_upstreams.json")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + ln.Addr().String()
	ln.Close()
	poisoned := map[string]string{
		"mnemo":     base + Prefix + "/mnemo",
		"atlassian": base + Prefix + "/atlassian",
		"vellum":    "http://127.0.0.1:18742/mcp",
	}
	b, _ := json.Marshal(poisoned)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := OpenUpstreamRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reg.Resolve([]claudia.MCPServer{
		{Name: "mnemo", URL: base + Prefix + "/mnemo"},
		{Name: "atlassian", URL: strings.Replace(base, "127.0.0.1", "localhost", 1) + Prefix + "/atlassian"},
		{Name: "vellum", URL: base + Prefix + "/vellum"},
	}, base+Prefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "vellum" || got[0].URL != "http://127.0.0.1:18742/mcp" {
		t.Fatalf("resolved = %+v; want only vellum, at its real URL", got)
	}
	var onDisk map[string]string
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if _, ok := onDisk["mnemo"]; ok {
		t.Fatalf("registry still names a self route for mnemo: %v", onDisk)
	}
	if _, ok := onDisk["atlassian"]; ok {
		t.Fatalf("registry still names a self route for atlassian: %v", onDisk)
	}
}

// A loopback URL to the proxy is ours whatever loopback spelling or name
// it uses: it is never remembered as a real upstream.
func TestT1035SelfRouteNeverRecordedAsUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_upstreams.json")
	reg, err := OpenUpstreamRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "http://127.0.0.1:13705" + Prefix
	got, err := reg.Resolve([]claudia.MCPServer{
		{Name: "mnemo", URL: "http://localhost:13705/upstream/mnemo"},
		{Name: "orthograph", URL: "http://[::1]:13705/upstream/orthograph"},
		{Name: "atlassian", URL: "http://127.0.0.1:13705/upstream/mnemo"},
	}, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("resolved = %+v; want every self route dropped", got)
	}
	if raw, err := os.ReadFile(path); err == nil && strings.Contains(string(raw), "13705") {
		t.Fatalf("registry recorded a self route: %s", raw)
	}
}
