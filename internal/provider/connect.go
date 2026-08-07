// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
)

// Connector attaches to a provider that is already running under its own
// lifecycle (its own daemon, launchd job, container). jevonsd never owns
// such a process — it dials the declared URL and watches liveness.
//
// Launch and connect are the same two shapes MCP uses (stdio-spawned vs
// HTTP endpoint), which is what 🎯T27.3 means by "both available,
// MCP-style": one contract, two attachment modes.
type Connector interface {
	Connect(ctx context.Context, d config.ProviderDecl) (Conn, error)
}

// Conn is an attachment to a running provider. Probe reports current
// liveness; Close releases hub-side resources only — never the provider,
// which jevonsd does not own.
type Conn interface {
	Probe(ctx context.Context) error
	Endpoint() string
	Close() error
}

// ProbeTimeout bounds a single connect-mode liveness check.
const ProbeTimeout = 5 * time.Second

// DefaultProbeInterval is how often a connected provider is re-probed
// when Lifecycle is not told otherwise.
const DefaultProbeInterval = 30 * time.Second

// HTTPConnector dials connect-mode providers over HTTP. A provider is
// considered live when its URL answers with any non-5xx status: the hub
// is checking that the endpoint is served, not asserting a route shape.
// The contract handshake (describe) rides the provider WebSocket and is
// 🎯T27.5's business, not liveness.
type HTTPConnector struct {
	// Client is the HTTP client for probes. Nil uses a default with
	// ProbeTimeout.
	Client *http.Client
}

// Connect validates the declared URL and returns an attachment. It does
// not probe — the runner does that, so a provider that is merely not up
// yet lands in backoff rather than being rejected outright.
func (c *HTTPConnector) Connect(ctx context.Context, d config.ProviderDecl) (Conn, error) {
	raw := strings.TrimSpace(d.ConnectURL())
	if raw == "" {
		return nil, fmt.Errorf("%w: provider %q: connect needs params.url", ErrUnrunnable, d.ID)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: provider %q: bad params.url %q: %v", ErrUnrunnable, d.ID, raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%w: provider %q: params.url scheme %q unsupported (want http|https)", ErrUnrunnable, d.ID, u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%w: provider %q: params.url %q has no host", ErrUnrunnable, d.ID, raw)
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: ProbeTimeout}
	}
	return &httpConn{url: u.String(), client: client}, nil
}

type httpConn struct {
	url    string
	client *http.Client
}

func (c *httpConn) Endpoint() string { return c.url }

func (c *httpConn) Probe(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("probe %s: %w", c.url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("probe %s: status %d", c.url, resp.StatusCode)
	}
	return nil
}

func (c *httpConn) Close() error { return nil }
