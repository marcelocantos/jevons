// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
)

// FeedHub is the hub's feed-ingestion half (🎯T27.5): it serves the
// provider feed channel (contract §4.1 wire frames over the dedicated
// /ws/provider socket), drives describe → subscribe per attached
// provider, folds inbound events into the aggregated live model
// (Registry), persists cursors/manifests (Store), and dispatches folded
// events to a sink — in production, Server.Broadcast to connected
// clients.
//
// Isolation is the load-bearing property: each attached provider is
// served by its own Attach call (one goroutine, owned by the WS
// handler), and event dispatch to the sink rides a bounded queue
// drained by a dedicated goroutine. A slow or stalled sink therefore
// degrades that provider's status (dropped dispatches, counted) but
// never blocks the read/fold loop, the hub, or other providers' feeds.
type FeedHub struct {
	registry *Registry
	store    *Store
	onEvent  func(providerID string, ev FeedEvent)
	onStatus func(FeedStatus)
	onUI     func()
	allowed  func(id string) bool
	log      *slog.Logger

	handshakeTimeout time.Duration
	queueSize        int

	mu     sync.Mutex
	conns  map[string]*feedConn  // provider id → active connection
	status map[string]FeedStatus // provider id → last status
}

// FeedHubArgs configures a FeedHub. Registry is required; everything
// else defaults to production behaviour.
type FeedHubArgs struct {
	// Registry receives folded events (the aggregated live model). Required.
	Registry *Registry
	// Store persists manifests and feed cursors. Nil disables persistence
	// (hermetic tests); a restart then replays from 0.
	Store *Store
	// OnEvent receives each folded event, dispatched via the bounded
	// queue. Nil means fold-only (no client broadcast).
	OnEvent func(providerID string, ev FeedEvent)
	// OnStatus observes feed-status transitions (ok/degraded/disconnected).
	// Called synchronously from hub goroutines — keep it cheap or hand off.
	OnStatus func(FeedStatus)
	// OnUI fires when the composed provider UI may have changed: a
	// provider with UI surfaces attached, or an event folded on a feed
	// one of its surfaces binds (🎯T27.6 reactive re-render). Called
	// synchronously from connection goroutines — keep it cheap.
	OnUI func()
	// Allowed gates which manifest ids may attach. Nil allows any id —
	// wire it to the enabled desired set in production.
	Allowed func(id string) bool
	// Logger receives attach/detach transitions. Nil uses slog.Default.
	Logger *slog.Logger
	// HandshakeTimeout bounds describe → describe_ok. Zero uses
	// DefaultHandshakeTimeout.
	HandshakeTimeout time.Duration
	// QueueSize bounds the per-provider dispatch queue. Zero uses
	// DefaultDispatchQueue.
	QueueSize int
}

// DefaultHandshakeTimeout bounds how long an attaching provider gets to
// answer describe before it is dropped as stalled.
const DefaultHandshakeTimeout = 10 * time.Second

// DefaultDispatchQueue is the per-provider bounded dispatch queue depth.
// Overflow drops the dispatch (the model still folds) and marks the
// provider's feed status degraded.
const DefaultDispatchQueue = 256

// modelCap bounds retained events per (provider, feed) in the live
// model. The model is last-known derived state (§5.3) — history lives
// with the provider — so retention here only needs to cover snapshot
// and recent-context needs, not durability.
const modelCap = 512

// Feed status states. Unlike lifecycle Phase (process liveness), these
// describe the feed channel: attached and folding (ok), attached but
// dropping dispatches or otherwise impaired (degraded), or no feed
// connection (disconnected).
const (
	FeedOK           = "ok"
	FeedDegraded     = "degraded"
	FeedDisconnected = "disconnected"
)

// FeedStatus is the owner-visible snapshot of one provider's feed
// channel. Reason is sticky within a connection (most recent
// impairment); Dropped counts dispatches lost to a full queue — the
// model itself still folded those events.
type FeedStatus struct {
	ID      string    `json:"id"`
	State   string    `json:"state"`
	Reason  string    `json:"reason,omitempty"`
	Feeds   []string  `json:"feeds,omitempty"` // subscribed feed names
	Events  int64     `json:"events"`          // folded this connection
	Dropped int64     `json:"dropped"`         // dispatches lost to overflow
	Since   time.Time `json:"since"`           // when the current state began
}

// FrameConn is one provider feed socket: JSON text frames in both
// directions. The server wraps its WebSocket in this; tests use an
// in-memory pipe.
type FrameConn interface {
	ReadFrame(ctx context.Context) ([]byte, error)
	WriteFrame(ctx context.Context, data []byte) error
	Close() error
}

// feedFrame is the §4.1 wire frame (superset of all ops).
type feedFrame struct {
	Op       string     `json:"op"`
	Manifest *Manifest  `json:"manifest,omitempty"` // describe_ok
	Feed     string     `json:"feed,omitempty"`     // subscribe
	From     int64      `json:"from,omitempty"`     // subscribe
	Event    *FeedEvent `json:"event,omitempty"`    // event
	Seq      int64      `json:"seq,omitempty"`      // ack
}

// feedConn is one live provider attachment.
type feedConn struct {
	conn  FrameConn
	queue chan FeedEvent
	// uiFeeds are feed names bound by this provider's UI surfaces —
	// folding an event on one of these triggers a UI re-render.
	uiFeeds map[string]bool
}

// NewFeedHub returns a hub ready to Attach provider connections.
func NewFeedHub(args FeedHubArgs) *FeedHub {
	if args.Registry == nil {
		panic("provider.NewFeedHub: Registry is required")
	}
	log := args.Logger
	if log == nil {
		log = slog.Default()
	}
	ht := args.HandshakeTimeout
	if ht <= 0 {
		ht = DefaultHandshakeTimeout
	}
	qs := args.QueueSize
	if qs <= 0 {
		qs = DefaultDispatchQueue
	}
	return &FeedHub{
		registry:         args.Registry,
		store:            args.Store,
		onEvent:          args.OnEvent,
		onStatus:         args.OnStatus,
		onUI:             args.OnUI,
		allowed:          args.Allowed,
		log:              log,
		handshakeTimeout: ht,
		queueSize:        qs,
		conns:            make(map[string]*feedConn),
		status:           make(map[string]FeedStatus),
	}
}

// SetDecls seeds/refreshes status placeholders for the declared desired
// set, so a provider that never attaches its feed channel surfaces as
// disconnected rather than being invisible. Removed providers' statuses
// are pruned unless a connection is live. Providers that leave the
// enabled set also lose their composed UI surfaces so the desktop head
// (🎯T27.7) drops their section on toggle — distinct from a transient
// feed disconnect, which keeps last-known surfaces.
func (h *FeedHub) SetDecls(decls []config.ProviderDecl) {
	h.mu.Lock()
	declared := make(map[string]bool, len(decls))
	for _, d := range decls {
		if !d.Enabled() {
			continue
		}
		declared[d.ID] = true
		if _, ok := h.status[d.ID]; !ok {
			h.status[d.ID] = FeedStatus{
				ID: d.ID, State: FeedDisconnected,
				Reason: "no feed connection", Since: time.Now(),
			}
		}
	}
	for id := range h.status {
		if !declared[id] && h.conns[id] == nil {
			delete(h.status, id)
		}
	}
	h.mu.Unlock()

	// Clear composed UI for providers not in the enabled desired set so
	// N enabled → N sections holds under toggle (registry is separate lock).
	uiChanged := false
	for pid := range h.registry.ComposedUI() {
		if !declared[pid] {
			h.registry.ClearSurfaces(pid)
			uiChanged = true
		}
	}
	if uiChanged {
		h.notifyUI()
	}
}

// Statuses returns the current feed status of every known provider,
// ordered deterministically by id.
func (h *FeedHub) Statuses() []FeedStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]FeedStatus, 0, len(h.status))
	for _, st := range h.status {
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// FeedSnapshot is the compact per-feed view broadcast to a connecting
// client: the last folded event and how many this hub has folded.
type FeedSnapshot struct {
	Last  *FeedEvent `json:"last,omitempty"`
	Count int        `json:"count"`
}

// Snapshot returns the aggregated live model as provider → feed →
// snapshot, for the client init frame.
func (h *FeedHub) Snapshot() map[string]map[string]FeedSnapshot {
	return h.registry.modelSnapshot()
}

// modelSnapshot summarises the folded model (same package: reaches into
// Registry state under its lock).
func (r *Registry) modelSnapshot() map[string]map[string]FeedSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]map[string]FeedSnapshot, len(r.model))
	for pid, feeds := range r.model {
		if len(feeds) == 0 {
			continue
		}
		fm := make(map[string]FeedSnapshot, len(feeds))
		for feed, evs := range feeds {
			if len(evs) == 0 {
				continue
			}
			last := evs[len(evs)-1]
			fm[feed] = FeedSnapshot{Last: &last, Count: len(evs)}
		}
		if len(fm) > 0 {
			out[pid] = fm
		}
	}
	return out
}

// Attach serves one provider feed connection to completion: handshake,
// subscribe, then fold events until the connection ends or ctx is
// cancelled. It blocks for the connection's lifetime — call it from the
// WS handler goroutine. The returned error describes why the attachment
// ended; a clean peer close returns the read error unwrapped.
func (h *FeedHub) Attach(ctx context.Context, conn FrameConn) error {
	defer conn.Close()

	m, err := h.handshake(ctx, conn)
	if err != nil {
		return err
	}
	id := m.ID

	if h.allowed != nil && !h.allowed(id) {
		h.setStatus(id, func(st *FeedStatus) {
			st.State = FeedDisconnected
			st.Reason = "provider not in enabled desired set"
		})
		return fmt.Errorf("provider %q not allowed", id)
	}

	if h.store != nil {
		if err := h.store.PutManifest(m); err != nil {
			h.log.Warn("provider feed: manifest persist failed", "provider", id, "err", err)
		}
	}

	fc := &feedConn{conn: conn, queue: make(chan FeedEvent, h.queueSize), uiFeeds: make(map[string]bool)}
	for _, s := range m.Capabilities.UI {
		for _, f := range s.Feeds {
			fc.uiFeeds[f] = true
		}
	}
	h.adopt(id, fc)
	defer h.drop(id, fc)

	// 🎯T27.6: store this provider's declared UI surfaces so the
	// server-side producer composes them, and re-render on attach.
	if len(m.Capabilities.UI) > 0 {
		h.registry.SetSurfaces(id, m.Capabilities.UI)
		h.notifyUI()
	}

	// Dispatcher: drains the bounded queue into the sink so a slow sink
	// (e.g. a wedged client write inside Broadcast) never blocks folding.
	var wg sync.WaitGroup
	wg.Go(func() {
		for ev := range fc.queue {
			if h.onEvent != nil {
				h.onEvent(id, ev)
			}
		}
	})
	defer wg.Wait()
	defer close(fc.queue)

	feeds, err := h.subscribe(ctx, conn, m)
	if err != nil {
		h.setConnStatus(id, fc, func(st *FeedStatus) {
			st.State = FeedDegraded
			st.Reason = "subscribe failed: " + err.Error()
		})
		return err
	}
	h.setConnStatus(id, fc, func(st *FeedStatus) {
		st.State = FeedOK
		st.Reason = ""
		st.Feeds = feeds
		st.Events = 0
		st.Dropped = 0
	})
	h.log.Info("provider feed attached", "provider", id, "feeds", feeds)

	for {
		data, err := conn.ReadFrame(ctx)
		if err != nil {
			h.setConnStatus(id, fc, func(st *FeedStatus) {
				st.State = FeedDisconnected
				st.Reason = "feed connection closed: " + err.Error()
			})
			h.log.Info("provider feed detached", "provider", id, "err", err)
			return err
		}
		var f feedFrame
		if err := json.Unmarshal(data, &f); err != nil {
			h.log.Warn("provider feed: bad frame", "provider", id, "err", err)
			continue
		}
		switch f.Op {
		case "event":
			if f.Event == nil {
				continue
			}
			h.ingest(id, fc, *f.Event)
		case "ack":
			// Optional provider-side cursor ack — informational.
		default:
			h.log.Debug("provider feed: unknown op", "provider", id, "op", f.Op)
		}
	}
}

// handshake drives describe → describe_ok under the handshake timeout.
// A provider that connects and never answers is dropped here — this is
// the stalled-provider bound, and it runs entirely inside this
// connection's goroutine, so a wedged provider cannot stall the hub.
func (h *FeedHub) handshake(ctx context.Context, conn FrameConn) (Manifest, error) {
	hctx, cancel := context.WithTimeout(ctx, h.handshakeTimeout)
	defer cancel()
	req, _ := json.Marshal(feedFrame{Op: "describe"})
	if err := conn.WriteFrame(hctx, req); err != nil {
		return Manifest{}, fmt.Errorf("describe write: %w", err)
	}
	data, err := conn.ReadFrame(hctx)
	if err != nil {
		return Manifest{}, fmt.Errorf("describe stalled: %w", err)
	}
	var f feedFrame
	if err := json.Unmarshal(data, &f); err != nil {
		return Manifest{}, fmt.Errorf("describe_ok parse: %w", err)
	}
	if f.Op != "describe_ok" || f.Manifest == nil {
		return Manifest{}, fmt.Errorf("expected describe_ok, got op %q", f.Op)
	}
	m := *f.Manifest
	if err := ValidateManifest(m); err != nil {
		return Manifest{}, fmt.Errorf("manifest invalid: %w", err)
	}
	return m, nil
}

// subscribe issues one subscribe frame per declared feed, resuming from
// the persisted cursor (from = last+1; 0 = full replay). Returns the
// subscribed feed names.
func (h *FeedHub) subscribe(ctx context.Context, conn FrameConn, m Manifest) ([]string, error) {
	var feeds []string
	for _, fc := range m.Capabilities.Feeds {
		var from int64
		if h.store != nil {
			last, err := h.store.GetCursor(m.ID, fc.Name)
			if err != nil {
				h.log.Warn("provider feed: cursor load failed", "provider", m.ID, "feed", fc.Name, "err", err)
			} else if last > 0 {
				from = last + 1
			}
		}
		raw, _ := json.Marshal(feedFrame{Op: "subscribe", Feed: fc.Name, From: from})
		if err := conn.WriteFrame(ctx, raw); err != nil {
			return nil, fmt.Errorf("subscribe %q: %w", fc.Name, err)
		}
		feeds = append(feeds, fc.Name)
	}
	return feeds, nil
}

// ingest folds one inbound event and dispatches it. Loop-safety and
// (origin, feed, seq) dedup live in Registry.Ingest; a full dispatch
// queue drops the dispatch (never blocks) and degrades the status.
func (h *FeedHub) ingest(id string, fc *feedConn, ev FeedEvent) {
	if !h.registry.Ingest(id, ev) {
		return // self-origin or already folded — dropped by design (§5.4)
	}
	if h.store != nil {
		if err := h.store.SetCursor(id, ev.Feed, ev.Seq); err != nil {
			h.log.Warn("provider feed: cursor persist failed", "provider", id, "feed", ev.Feed, "err", err)
		}
	}
	select {
	case fc.queue <- ev:
		h.setConnStatus(id, fc, func(st *FeedStatus) { st.Events++ })
	default:
		h.setConnStatus(id, fc, func(st *FeedStatus) {
			st.Events++
			st.Dropped++
			st.State = FeedDegraded
			st.Reason = "dispatch queue full (slow consumer)"
		})
	}
	// Reactive re-render (§6.2): feed event → aggregated model →
	// surface re-render, for feeds a surface of this provider binds.
	if fc.uiFeeds[ev.Feed] {
		h.notifyUI()
	}
}

// notifyUI signals the composed provider UI may have changed.
func (h *FeedHub) notifyUI() {
	if h.onUI != nil {
		h.onUI()
	}
}

// ComposedUI exposes the aggregated provider surfaces for the 🎯T27.6
// server-side producer.
func (h *FeedHub) ComposedUI() map[string][]UISurface {
	return h.registry.ComposedUI()
}

// SendAction relays one client UI action to the attached provider that
// owns the surface (contract §4.1 action frame). Errors if the provider
// has no live feed connection.
func (h *FeedHub) SendAction(ctx context.Context, providerID, surface, action, value string) error {
	h.mu.Lock()
	fc := h.conns[providerID]
	h.mu.Unlock()
	if fc == nil {
		return fmt.Errorf("provider %q not attached", providerID)
	}
	raw, _ := json.Marshal(map[string]string{
		"op": "action", "surface": surface, "action": action, "value": value,
	})
	return fc.conn.WriteFrame(ctx, raw)
}

// adopt registers the connection as current for id, replacing (and
// closing) any previous one so a provider reconnecting after a silent
// drop is not locked out by its own stale socket.
func (h *FeedHub) adopt(id string, fc *feedConn) {
	h.mu.Lock()
	prev := h.conns[id]
	h.conns[id] = fc
	h.mu.Unlock()
	if prev != nil {
		_ = prev.conn.Close()
	}
}

// drop unregisters the connection if it is still current.
func (h *FeedHub) drop(id string, fc *feedConn) {
	h.mu.Lock()
	if h.conns[id] == fc {
		delete(h.conns, id)
	}
	h.mu.Unlock()
}

// setStatus mutates one provider's status under the lock and notifies
// the observer when the state string changed.
func (h *FeedHub) setStatus(id string, mut func(*FeedStatus)) {
	h.applyStatus(id, nil, mut)
}

// setConnStatus is setStatus scoped to one connection: a superseded
// socket (replaced by adopt) reporting its own death must not stomp the
// status the replacement connection now owns.
func (h *FeedHub) setConnStatus(id string, fc *feedConn, mut func(*FeedStatus)) {
	h.applyStatus(id, fc, mut)
}

func (h *FeedHub) applyStatus(id string, onlyIfCurrent *feedConn, mut func(*FeedStatus)) {
	h.mu.Lock()
	if onlyIfCurrent != nil && h.conns[id] != onlyIfCurrent {
		h.mu.Unlock()
		return
	}
	st, ok := h.status[id]
	if !ok {
		st = FeedStatus{ID: id, Since: time.Now()}
	}
	before := st.State
	mut(&st)
	if st.State != before {
		st.Since = time.Now()
	}
	h.status[id] = st
	notify := h.onStatus
	h.mu.Unlock()
	if notify != nil && st.State != before {
		notify(st)
	}
}
