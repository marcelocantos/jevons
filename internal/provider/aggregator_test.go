// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/config"
)

// fakeSink records re-exported tools by qualified name.
type fakeSink struct {
	mu    sync.Mutex
	tools map[string]func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
	descs map[string]string
}

func newFakeSink() *fakeSink {
	return &fakeSink{
		tools: make(map[string]func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)),
		descs: make(map[string]string),
	}
}

func (s *fakeSink) AddProviderTool(t mcp.Tool, h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[t.Name] = h
	s.descs[t.Name] = t.Description
}

func (s *fakeSink) RemoveProviderTools(names ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range names {
		delete(s.tools, n)
		delete(s.descs, n)
	}
}

func (s *fakeSink) has(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.tools[name]
	return ok
}

func (s *fakeSink) desc(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.descs[name]
}

func (s *fakeSink) handler(name string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tools[name]
}

// fakeMCPClient serves a fixed tool list and records calls.
type fakeMCPClient struct {
	tools []mcp.Tool

	mu       sync.Mutex
	lastName string
	lastArgs map[string]any
	closed   bool
}

func (c *fakeMCPClient) ListTools(ctx context.Context) ([]mcp.Tool, error) { return c.tools, nil }

func (c *fakeMCPClient) CallTool(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastName = name
	c.lastArgs = args
	return mcp.NewToolResultText("ok"), nil
}

func (c *fakeMCPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *fakeMCPClient) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func waitAgg(t *testing.T, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(msg)
}

func echoToolDecls(id, url string) []config.ProviderDecl {
	return []config.ProviderDecl{{
		ID:        id,
		Transport: config.ProviderTransportConnect,
		Params:    map[string]any{"url": url, "mcp_url": url},
	}}
}

func newTestAggregator(sink *fakeSink, dial MCPDialer) *Aggregator {
	return NewAggregator(AggregatorArgs{Sink: sink, Dial: dial, RetryInterval: 10 * time.Millisecond})
}

func TestAggregatorAttachAndDetach(t *testing.T) {
	sink := newFakeSink()
	client := &fakeMCPClient{tools: []mcp.Tool{
		{Name: "echo", Description: "Echo a string"},
		{Name: "sum", Description: "Add numbers"},
	}}
	a := newTestAggregator(sink, func(ctx context.Context, url string) (MCPToolClient, error) {
		return client, nil
	})
	a.SetDecls(echoToolDecls("p1", "http://x"))

	a.HandlePhase(Health{ID: "p1", Phase: PhaseRunning})
	waitAgg(t, "tools never aggregated", func() bool { return sink.has("p1__echo") && sink.has("p1__sum") })

	if d := sink.desc("p1__echo"); !strings.HasPrefix(d, "[provider p1] ") {
		t.Errorf("description not attributed: %q", d)
	}
	if got := len(a.Attached()); got != 2 {
		t.Errorf("Attached() = %d tools, want 2", got)
	}

	a.HandlePhase(Health{ID: "p1", Phase: PhaseBackoff})
	waitAgg(t, "tools not withdrawn on phase down", func() bool { return !sink.has("p1__echo") && !sink.has("p1__sum") })
	waitAgg(t, "client not closed on detach", client.isClosed)
	if got := len(a.Attached()); got != 0 {
		t.Errorf("Attached() = %d tools after detach, want 0", got)
	}
}

func TestAggregatorCallForwardsBareNameAndArgs(t *testing.T) {
	sink := newFakeSink()
	client := &fakeMCPClient{tools: []mcp.Tool{{Name: "echo"}}}
	a := newTestAggregator(sink, func(ctx context.Context, url string) (MCPToolClient, error) {
		return client, nil
	})
	a.SetDecls(echoToolDecls("p1", "http://x"))
	a.HandlePhase(Health{ID: "p1", Phase: PhaseRunning})
	waitAgg(t, "tool never aggregated", func() bool { return sink.has("p1__echo") })

	h := sink.handler("p1__echo")
	var req mcp.CallToolRequest
	req.Params.Name = "p1__echo"
	req.Params.Arguments = map[string]any{"text": "hi"}
	if _, err := h(context.Background(), req); err != nil {
		t.Fatalf("handler: %v", err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.lastName != "echo" {
		t.Errorf("provider called with %q, want bare \"echo\"", client.lastName)
	}
	if client.lastArgs["text"] != "hi" {
		t.Errorf("args not forwarded: %v", client.lastArgs)
	}
}

func TestAggregatorSkipsProvidersWithoutMCPURL(t *testing.T) {
	sink := newFakeSink()
	var dials atomic.Int32
	a := newTestAggregator(sink, func(ctx context.Context, url string) (MCPToolClient, error) {
		dials.Add(1)
		return &fakeMCPClient{}, nil
	})
	a.SetDecls([]config.ProviderDecl{{
		ID:        "plain",
		Transport: config.ProviderTransportConnect,
		Params:    map[string]any{"url": "http://x"},
	}})
	a.HandlePhase(Health{ID: "plain", Phase: PhaseRunning})
	time.Sleep(30 * time.Millisecond)
	if n := dials.Load(); n != 0 {
		t.Errorf("dialed %d times for a provider with no mcp_url, want 0", n)
	}
}

func TestAggregatorSetDeclsWithdrawsRemovedProvider(t *testing.T) {
	sink := newFakeSink()
	a := newTestAggregator(sink, func(ctx context.Context, url string) (MCPToolClient, error) {
		return &fakeMCPClient{tools: []mcp.Tool{{Name: "echo"}}}, nil
	})
	a.SetDecls(echoToolDecls("p1", "http://x"))
	a.HandlePhase(Health{ID: "p1", Phase: PhaseRunning})
	waitAgg(t, "tool never aggregated", func() bool { return sink.has("p1__echo") })

	a.SetDecls(nil)
	waitAgg(t, "tools not withdrawn after decl removal", func() bool { return !sink.has("p1__echo") })
}

func TestAggregatorDisabledDeclWithdraws(t *testing.T) {
	sink := newFakeSink()
	a := newTestAggregator(sink, func(ctx context.Context, url string) (MCPToolClient, error) {
		return &fakeMCPClient{tools: []mcp.Tool{{Name: "echo"}}}, nil
	})
	decls := echoToolDecls("p1", "http://x")
	a.SetDecls(decls)
	a.HandlePhase(Health{ID: "p1", Phase: PhaseRunning})
	waitAgg(t, "tool never aggregated", func() bool { return sink.has("p1__echo") })

	off := false
	decls[0].Enable = &off
	a.SetDecls(decls)
	waitAgg(t, "tools not withdrawn after disable", func() bool { return !sink.has("p1__echo") })
}

func TestAggregatorRetriesUntilEndpointUp(t *testing.T) {
	sink := newFakeSink()
	var dials atomic.Int32
	a := newTestAggregator(sink, func(ctx context.Context, url string) (MCPToolClient, error) {
		if dials.Add(1) < 3 {
			return nil, errors.New("connection refused")
		}
		return &fakeMCPClient{tools: []mcp.Tool{{Name: "echo"}}}, nil
	})
	a.SetDecls(echoToolDecls("p1", "http://x"))
	a.HandlePhase(Health{ID: "p1", Phase: PhaseRunning})
	waitAgg(t, "tool never aggregated after retries", func() bool { return sink.has("p1__echo") })
	if n := dials.Load(); n < 3 {
		t.Errorf("dialed %d times, want ≥3", n)
	}
}

func TestAggregatorRepeatedRunningDoesNotRedial(t *testing.T) {
	sink := newFakeSink()
	var dials atomic.Int32
	a := newTestAggregator(sink, func(ctx context.Context, url string) (MCPToolClient, error) {
		dials.Add(1)
		return &fakeMCPClient{tools: []mcp.Tool{{Name: "echo"}}}, nil
	})
	a.SetDecls(echoToolDecls("p1", "http://x"))
	a.HandlePhase(Health{ID: "p1", Phase: PhaseRunning})
	waitAgg(t, "tool never aggregated", func() bool { return sink.has("p1__echo") })

	// Connect-mode re-probes re-assert Running on every healthy poll.
	a.HandlePhase(Health{ID: "p1", Phase: PhaseRunning})
	a.HandlePhase(Health{ID: "p1", Phase: PhaseRunning})
	time.Sleep(30 * time.Millisecond)
	if n := dials.Load(); n != 1 {
		t.Errorf("dialed %d times, want 1", n)
	}
}

func TestAggregatorShutdownWithdrawsEverything(t *testing.T) {
	sink := newFakeSink()
	client := &fakeMCPClient{tools: []mcp.Tool{{Name: "echo"}}}
	a := newTestAggregator(sink, func(ctx context.Context, url string) (MCPToolClient, error) {
		return client, nil
	})
	a.SetDecls(echoToolDecls("p1", "http://x"))
	a.HandlePhase(Health{ID: "p1", Phase: PhaseRunning})
	waitAgg(t, "tool never aggregated", func() bool { return sink.has("p1__echo") })

	a.Shutdown()
	// Shutdown waits for cleanup, so the sink must already be empty.
	if sink.has("p1__echo") {
		t.Error("tools survived Shutdown")
	}
	if !client.isClosed() {
		t.Error("client not closed by Shutdown")
	}
	// Post-shutdown phases must not resurrect anything.
	a.HandlePhase(Health{ID: "p1", Phase: PhaseRunning})
	time.Sleep(20 * time.Millisecond)
	if sink.has("p1__echo") {
		t.Error("tools reappeared after Shutdown")
	}
}
