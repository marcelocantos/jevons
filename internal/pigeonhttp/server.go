// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package pigeonhttp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/coder/websocket"
)

// Server turns pigeon primary-stream frames into HTTP and WebSocket
// traffic against Target (normally the daemon's own loopback origin).
type Server struct {
	// Target is the base URL of the daemon (e.g. http://127.0.0.1:13705).
	// Required for WebSocket upgrades. Plain HTTP can also use Handler.
	Target string

	// Handler, when set, serves plain HTTP without a network hop.
	// WebSocket still dials Target.
	Handler http.Handler

	// Log receives non-fatal per-request failures. Optional.
	Log *slog.Logger
}

// Serve runs the gateway until ctx is cancelled or the conn dies.
// The first client frame must be OpHello; anything else is refused.
func (s *Server) Serve(ctx context.Context, c Conn) error {
	first, err := RecvFrame(ctx, c)
	if err != nil {
		return err
	}
	if first.Op != OpHello {
		_ = SendFrame(ctx, c, Frame{Op: OpError, Error: "expected hello, got " + first.Op})
		return fmt.Errorf("pigeonhttp: first op %q, want hello", first.Op)
	}
	if err := SendFrame(ctx, c, Frame{Op: OpHelloOK}); err != nil {
		return err
	}

	var (
		mu  sync.Mutex
		wss = map[string]*websocket.Conn{}
	)
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for id, ws := range wss {
			_ = ws.Close(websocket.StatusNormalClosure, "")
			delete(wss, id)
		}
	}()

	for {
		f, err := RecvFrame(ctx, c)
		if err != nil {
			return err
		}
		switch f.Op {
		case OpHTTP:
			go s.handleHTTP(ctx, c, f)
		case OpWSOpen:
			go s.handleWSOpen(ctx, c, f, &mu, wss)
		case OpWSMsg:
			s.handleWSMsg(ctx, f, &mu, wss)
		case OpWSClose:
			s.handleWSClose(f, &mu, wss)
		default:
			s.log("unknown op", "op", f.Op, "id", f.ID)
			if f.ID != "" {
				_ = SendFrame(ctx, c, Frame{Op: OpError, ID: f.ID, Error: "unknown op " + f.Op})
			}
		}
	}
}

func (s *Server) handleHTTP(ctx context.Context, c Conn, f Frame) {
	body, err := f.Body()
	if err != nil {
		_ = SendFrame(ctx, c, Frame{Op: OpError, ID: f.ID, Error: "bad body_b64: " + err.Error()})
		return
	}
	path := f.Path
	if path == "" {
		path = "/"
	}
	req, err := http.NewRequestWithContext(ctx, f.Method, path, bytes.NewReader(body))
	if err != nil {
		_ = SendFrame(ctx, c, Frame{Op: OpError, ID: f.ID, Error: err.Error()})
		return
	}
	for k, v := range f.Headers {
		req.Header.Set(k, v)
	}

	var (
		status   int
		headers  http.Header
		respBody []byte
	)
	if s.Handler != nil {
		rec := httptest.NewRecorder()
		s.Handler.ServeHTTP(rec, req)
		status = rec.Code
		headers = rec.Header().Clone()
		respBody = rec.Body.Bytes()
	} else {
		if s.Target == "" {
			_ = SendFrame(ctx, c, Frame{Op: OpError, ID: f.ID, Error: "no target or handler"})
			return
		}
		u := strings.TrimRight(s.Target, "/") + path
		out, err := http.NewRequestWithContext(ctx, f.Method, u, bytes.NewReader(body))
		if err != nil {
			_ = SendFrame(ctx, c, Frame{Op: OpError, ID: f.ID, Error: err.Error()})
			return
		}
		out.Header = req.Header.Clone()
		resp, err := http.DefaultClient.Do(out)
		if err != nil {
			_ = SendFrame(ctx, c, Frame{Op: OpError, ID: f.ID, Error: err.Error()})
			return
		}
		defer resp.Body.Close()
		status = resp.StatusCode
		headers = resp.Header.Clone()
		respBody, err = io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		if err != nil {
			_ = SendFrame(ctx, c, Frame{Op: OpError, ID: f.ID, Error: err.Error()})
			return
		}
	}

	hdr := map[string]string{}
	for k, vs := range headers {
		if len(vs) > 0 {
			hdr[k] = vs[0]
		}
	}
	if err := SendFrame(ctx, c, Frame{
		Op:      OpHTTPResp,
		ID:      f.ID,
		Status:  status,
		Headers: hdr,
		BodyB64: WithBody(respBody),
		Fin:     true,
	}); err != nil {
		s.log("http-resp send failed", "id", f.ID, "err", err)
	}
}

func (s *Server) handleWSOpen(ctx context.Context, c Conn, f Frame, mu *sync.Mutex, wss map[string]*websocket.Conn) {
	if s.Target == "" {
		_ = SendFrame(ctx, c, Frame{Op: OpWSOpenErr, ID: f.ID, Error: "no target for websocket"})
		return
	}
	path := f.Path
	if path == "" {
		path = "/ws/mux"
	}
	base := strings.TrimRight(s.Target, "/")
	wsURL := "ws" + strings.TrimPrefix(base, "http") + path
	opts := &websocket.DialOptions{}
	if len(f.Headers) > 0 {
		opts.HTTPHeader = http.Header{}
		for k, v := range f.Headers {
			opts.HTTPHeader.Set(k, v)
		}
	}
	ws, resp, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		msg := err.Error()
		if resp != nil {
			msg = fmt.Sprintf("%s (http %d)", msg, resp.StatusCode)
		}
		_ = SendFrame(ctx, c, Frame{Op: OpWSOpenErr, ID: f.ID, Error: msg})
		return
	}
	mu.Lock()
	wss[f.ID] = ws
	mu.Unlock()
	if err := SendFrame(ctx, c, Frame{Op: OpWSOpenOK, ID: f.ID}); err != nil {
		_ = ws.Close(websocket.StatusNormalClosure, "")
		mu.Lock()
		delete(wss, f.ID)
		mu.Unlock()
		return
	}
	go s.pumpWS(ctx, c, f.ID, ws, mu, wss)
}

func (s *Server) pumpWS(ctx context.Context, c Conn, id string, ws *websocket.Conn, mu *sync.Mutex, wss map[string]*websocket.Conn) {
	defer func() {
		_ = ws.Close(websocket.StatusNormalClosure, "")
		mu.Lock()
		delete(wss, id)
		mu.Unlock()
		_ = SendFrame(ctx, c, Frame{Op: OpWSClose, ID: id, Code: 1000})
	}()
	for {
		mt, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		f := Frame{
			Op:       OpWSMsg,
			ID:       id,
			ChunkB64: WithBody(data),
			Text:     mt == websocket.MessageText,
		}
		if err := SendFrame(ctx, c, f); err != nil {
			return
		}
	}
}

func (s *Server) handleWSMsg(ctx context.Context, f Frame, mu *sync.Mutex, wss map[string]*websocket.Conn) {
	data, err := f.Chunk()
	if err != nil {
		return
	}
	mu.Lock()
	ws := wss[f.ID]
	mu.Unlock()
	if ws == nil {
		return
	}
	mt := websocket.MessageBinary
	if f.Text {
		mt = websocket.MessageText
	}
	_ = ws.Write(ctx, mt, data)
}

func (s *Server) handleWSClose(f Frame, mu *sync.Mutex, wss map[string]*websocket.Conn) {
	mu.Lock()
	ws := wss[f.ID]
	delete(wss, f.ID)
	mu.Unlock()
	if ws != nil {
		_ = ws.Close(websocket.StatusNormalClosure, f.Reason)
	}
}

func (s *Server) log(msg string, args ...any) {
	if s.Log != nil {
		s.Log.Info("pigeonhttp: "+msg, args...)
	}
}

// IsHello reports whether raw bytes are a gateway hello frame.
// Used by the relay path to decide gateway vs legacy remote protocol.
func IsHello(data []byte) bool {
	f, err := Decode(data)
	return err == nil && f.Op == OpHello
}
