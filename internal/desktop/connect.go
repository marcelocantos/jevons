// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/marcelocantos/jevons/internal/provider"
)

// Client connects a Head to a running jevonsd over /ws/remote and applies
// composed provider view frames as they arrive. It is the product path
// the menu-bar binary uses (🎯T27.7).
type Client struct {
	// BaseURL is the jevonsd origin, e.g. "http://127.0.0.1:13705".
	BaseURL string
	// Head receives section updates. Required.
	Head *Head
	// HTTP is the client used for dial upgrades. Nil uses http.DefaultClient.
	HTTP *http.Client
	// DialTimeout bounds the initial websocket dial. Zero uses 10s.
	DialTimeout time.Duration
}

// DefaultBaseURL is the daily jevonsd origin (Universe A).
const DefaultBaseURL = "http://127.0.0.1:13705"

// Run dials /ws/remote and applies frames until ctx is cancelled or the
// connection ends. On a successful dial and first frame (init or view)
// the head is marked connected. View frames with slot "providers" rebuild
// sections from the composed root.
func (c *Client) Run(ctx context.Context) error {
	if c.Head == nil {
		return fmt.Errorf("desktop.Client: Head is required")
	}
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		_ = c.applyFrame(data)
	}
}

// FetchSectionsOnce dials, waits for the seeded view (or settles after
// init with zero sections), and returns the head's model. Used by the
// binary's -json-once mode and live probes.
func (c *Client) FetchSectionsOnce(ctx context.Context) (MenuModel, error) {
	if c.Head == nil {
		c.Head = NewHead()
	}
	conn, err := c.dial(ctx)
	if err != nil {
		return MenuModel{}, err
	}
	defer conn.CloseNow()

	timeout := c.DialTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	sawInit := false
	settle := time.NewTimer(250 * time.Millisecond)
	defer settle.Stop()
	for {
		readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
		_, data, err := conn.Read(readCtx)
		readCancel()
		if err != nil {
			if sawInit && c.Head.Model().Connected {
				return c.Head.Model(), nil
			}
			return MenuModel{}, err
		}
		var peek struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(data, &peek)
		_ = c.applyFrame(data)
		switch peek.Type {
		case "init":
			sawInit = true
			if !settle.Stop() {
				select {
				case <-settle.C:
				default:
				}
			}
			settle.Reset(250 * time.Millisecond)
		case "view":
			return c.Head.Model(), nil
		default:
			if sawInit {
				select {
				case <-settle.C:
					return c.Head.Model(), nil
				default:
				}
			}
		}
	}
}

func (c *Client) dial(ctx context.Context) (*websocket.Conn, error) {
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	wsURL, err := toWSURL(base, "/ws/remote")
	if err != nil {
		return nil, err
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	timeout := c.DialTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{
		HTTPClient: httpClient,
	})
	if err != nil {
		return nil, fmt.Errorf("desktop head dial %s: %w", wsURL, err)
	}
	conn.SetReadLimit(1 << 20)
	return conn, nil
}

// applyFrame decodes one WS JSON frame and updates the head when relevant.
func (c *Client) applyFrame(data []byte) error {
	var envelope struct {
		Type string `json:"type"`
		Slot string `json:"slot"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	switch envelope.Type {
	case "init":
		c.Head.MarkConnected()
		return nil
	case "view":
		if envelope.Slot != "" && envelope.Slot != "providers" {
			return nil
		}
		var full struct {
			Root provider.ViewNode `json:"root"`
		}
		if err := json.Unmarshal(data, &full); err != nil {
			return err
		}
		c.Head.ApplyComposedRoot(full.Root)
		return nil
	case "dismiss":
		if envelope.Slot == "providers" || envelope.Slot == "" {
			c.Head.ApplySurfaces(nil)
		}
		return nil
	default:
		return nil
	}
}

func toWSURL(base, path string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
		// already
	default:
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}
