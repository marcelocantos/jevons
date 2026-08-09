// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/config"
)

// ToolSink is where aggregated provider tools land — in production, the
// jevonsd MCP server that the overseer and workers consume on /mcp
// (internal/mcpserver satisfies this structurally). Handlers use the
// bare mcp-go signature so this package does not import the server side.
type ToolSink interface {
	AddProviderTool(t mcp.Tool, h func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error))
	RemoveProviderTools(names ...string)
}

// Aggregator is the hub's MCP aggregation half (🎯T27.4): when a
// supervised provider that declares an MCP endpoint (params.mcp_url)
// becomes Running, the aggregator dials it, lists its tools, and
// re-exports them into the sink namespaced as "{provider}__{name}" and
// attributed in the description. When the provider leaves Running —
// crash, probe failure, disable, removal, shutdown — its tools are
// withdrawn.
//
// Wire HandlePhase as the Lifecycle OnPhase observer and SetDecls
// alongside every Reconcile; enable/disable and config reload then flow
// through with no extra plumbing, because any params change (including
// mcp_url) restarts the provider and replays the phase transitions.
type Aggregator struct {
	sink  ToolSink
	dial  MCPDialer
	log   *slog.Logger
	retry time.Duration

	mu      sync.Mutex
	closed  bool
	mcpURLs map[string]string // provider id → declared mcp_url
	attach  map[string]*attachment
}

// AggregatorArgs configures an Aggregator. Sink is required; everything
// else defaults to production behaviour.
type AggregatorArgs struct {
	// Sink receives re-exported tools. Required.
	Sink ToolSink
	// Dial opens provider MCP connections. Nil uses DialMCP.
	Dial MCPDialer
	// Logger receives attach/detach transitions. Nil uses slog.Default.
	Logger *slog.Logger
	// RetryInterval is the wait between attach attempts while a Running
	// provider's MCP endpoint is not answering yet. Zero uses 3s.
	RetryInterval time.Duration
}

// DefaultAttachRetry is the wait between attach attempts when a Running
// provider's MCP endpoint is not up yet (it may bind its port a moment
// after the process starts).
const DefaultAttachRetry = 3 * time.Second

// attachment is one provider's live aggregation: a dial/list/register
// goroutine that owns the client and the registered tool names, and
// cleans both up when its context is cancelled.
type attachment struct {
	id     string
	url    string
	cancel context.CancelFunc
	done   chan struct{}

	mu        sync.Mutex
	qualified []string
}

// NewAggregator returns an aggregator with no providers attached.
func NewAggregator(args AggregatorArgs) *Aggregator {
	a := &Aggregator{
		sink:    args.Sink,
		dial:    args.Dial,
		log:     args.Logger,
		retry:   args.RetryInterval,
		mcpURLs: make(map[string]string),
		attach:  make(map[string]*attachment),
	}
	if a.dial == nil {
		a.dial = DialMCP
	}
	if a.log == nil {
		a.log = slog.Default()
	}
	if a.retry <= 0 {
		a.retry = DefaultAttachRetry
	}
	return a
}

// SetDecls records the desired set's declared MCP endpoints. Call it
// with the same declarations every Lifecycle.Reconcile sees, before the
// reconcile, so phase transitions find a fresh map. Providers that lose
// their declaration (or their mcp_url) are detached here; new or
// changed endpoints attach on the provider's next Running transition,
// which the Lifecycle guarantees because a params change restarts it.
func (a *Aggregator) SetDecls(decls []config.ProviderDecl) {
	urls := make(map[string]string)
	for _, d := range decls {
		if d.Enabled() && d.MCPURL() != "" {
			urls[d.ID] = d.MCPURL()
		}
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.mcpURLs = urls
	var stale []*attachment
	for id, at := range a.attach {
		if urls[id] != at.url {
			stale = append(stale, at)
			delete(a.attach, id)
		}
	}
	a.mu.Unlock()
	for _, at := range stale {
		at.cancel()
	}
}

// HandlePhase is the Lifecycle OnPhase observer: Running attaches (if an
// MCP endpoint is declared), every other phase detaches. It never
// blocks — dial/list runs in the attachment's own goroutine, and
// teardown is owned by that goroutine's cleanup.
func (a *Aggregator) HandlePhase(h Health) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	url := a.mcpURLs[h.ID]
	cur := a.attach[h.ID]
	if h.Phase != PhaseRunning || url == "" {
		delete(a.attach, h.ID)
		a.mu.Unlock()
		if cur != nil {
			cur.cancel()
		}
		return
	}
	if cur != nil && cur.url == url {
		a.mu.Unlock()
		return // connect-mode re-probes re-assert Running; already attached
	}
	ctx, cancel := context.WithCancel(context.Background())
	at := &attachment{id: h.ID, url: url, cancel: cancel, done: make(chan struct{})}
	a.attach[h.ID] = at
	a.mu.Unlock()
	if cur != nil {
		cur.cancel()
	}
	go func() {
		// A replaced attachment (respecified mcp_url) may share qualified
		// tool names with its successor; wait out its cleanup so the old
		// withdrawal cannot land after the new registration.
		if cur != nil {
			<-cur.done
		}
		a.runAttach(ctx, at)
	}()
}

// runAttach dials the provider's MCP endpoint, retrying while the
// attachment lives, registers its tools on success, then holds until
// detach. The deferred cleanup withdraws the tools and closes the
// client, so registration and withdrawal cannot get out of sync.
func (a *Aggregator) runAttach(ctx context.Context, at *attachment) {
	defer close(at.done)
	var client MCPToolClient
	defer func() {
		at.mu.Lock()
		names := at.qualified
		at.qualified = nil
		at.mu.Unlock()
		if len(names) > 0 {
			a.sink.RemoveProviderTools(names...)
			a.log.Info("provider mcp tools withdrawn", "provider", at.id, "tools", len(names))
		}
		if client != nil {
			client.Close()
		}
	}()

	for {
		c, err := a.dial(ctx, at.url)
		if err == nil {
			tools, lerr := c.ListTools(ctx)
			if lerr == nil {
				client = c
				a.register(at, c, tools)
				<-ctx.Done()
				return
			}
			err = lerr
			c.Close()
		}
		if ctx.Err() != nil {
			return
		}
		a.log.Warn("provider mcp attach failed — retrying", "provider", at.id, "url", at.url, "err", err)
		t := time.NewTimer(a.retry)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return
		}
	}
}

// register re-exports the provider's tools into the sink, namespaced
// "{provider}__{name}" (the same qualified form Registry.CallTool
// splits) and attributed in the description.
func (a *Aggregator) register(at *attachment, client MCPToolClient, tools []mcp.Tool) {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		bare := t.Name
		t.Name = at.id + "__" + bare
		t.Description = fmt.Sprintf("[provider %s] %s", at.id, t.Description)
		a.sink.AddProviderTool(t, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return client.CallTool(ctx, bare, req.GetArguments())
		})
		names = append(names, t.Name)
	}
	at.mu.Lock()
	at.qualified = names
	at.mu.Unlock()
	a.log.Info("provider mcp tools aggregated", "provider", at.id, "url", at.url, "tools", len(names))
}

// Attached reports the currently re-exported tools, sorted by qualified
// name — the observability surface for /api/providers and logs.
func (a *Aggregator) Attached() []Tool {
	a.mu.Lock()
	ats := make([]*attachment, 0, len(a.attach))
	for _, at := range a.attach {
		ats = append(ats, at)
	}
	a.mu.Unlock()
	var out []Tool
	for _, at := range ats {
		at.mu.Lock()
		for _, q := range at.qualified {
			out = append(out, Tool{Provider: at.id, Name: q[len(at.id)+2:], Qualified: q})
		}
		at.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Qualified < out[j].Qualified })
	return out
}

// Shutdown detaches every provider and waits for their cleanups, so a
// caller that shuts down then inspects the sink sees no residue.
func (a *Aggregator) Shutdown() {
	a.mu.Lock()
	a.closed = true
	ats := make([]*attachment, 0, len(a.attach))
	for _, at := range a.attach {
		ats = append(ats, at)
	}
	a.attach = make(map[string]*attachment)
	a.mu.Unlock()
	for _, at := range ats {
		at.cancel()
	}
	for _, at := range ats {
		<-at.done
	}
}
