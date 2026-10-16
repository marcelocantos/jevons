// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package pigeonhttp_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/marcelocantos/jevons/internal/pigeonhttp"
)

func TestT628FrameRoundTrip(t *testing.T) {
	f := pigeonhttp.Frame{
		Op:      pigeonhttp.OpHTTP,
		ID:      "r1",
		Method:  "GET",
		Path:    "/",
		Headers: map[string]string{"Accept": "text/html"},
		BodyB64: pigeonhttp.WithBody([]byte("hi")),
	}
	raw, err := pigeonhttp.Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pigeonhttp.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Op != f.Op || got.ID != f.ID || got.Method != f.Method || got.Path != f.Path {
		t.Fatalf("got %+v", got)
	}
	body, err := got.Body()
	if err != nil || string(body) != "hi" {
		t.Fatalf("body=%q err=%v", body, err)
	}
}

func TestT628IsHello(t *testing.T) {
	raw, _ := pigeonhttp.Encode(pigeonhttp.Frame{Op: pigeonhttp.OpHello})
	if !pigeonhttp.IsHello(raw) {
		t.Fatal("expected hello")
	}
	raw, _ = pigeonhttp.Encode(pigeonhttp.Frame{Op: pigeonhttp.OpHTTP, ID: "x"})
	if pigeonhttp.IsHello(raw) {
		t.Fatal("http is not hello")
	}
	if pigeonhttp.IsHello([]byte("not-json")) {
		t.Fatal("garbage")
	}
}

func TestT628GatewayHTTPAndMux(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<!doctype html><div id=root>react-cockpit</div>")
	})
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "1"})
	})
	mux.HandleFunc("/ws/mux", func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer ws.Close(websocket.StatusNormalClosure, "")
		ctx := r.Context()
		_ = ws.Write(ctx, websocket.MessageText, []byte(`{"type":"hello","channel":"transcript:jevons"}`))
		_, msg, err := ws.Read(ctx)
		if err != nil {
			return
		}
		_ = ws.Write(ctx, websocket.MessageText, []byte(`{"echo":`+string(msg)+`}`))
		for {
			if _, _, err := ws.Read(ctx); err != nil {
				return
			}
		}
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	pair := pigeonhttp.NewPipePair()
	srv := &pigeonhttp.Server{Target: ts.URL, Handler: mux}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx, pair.B) }()

	if err := pigeonhttp.SendFrame(ctx, pair.A, pigeonhttp.Frame{Op: pigeonhttp.OpHello}); err != nil {
		t.Fatal(err)
	}
	ok, err := pigeonhttp.RecvFrame(ctx, pair.A)
	if err != nil || ok.Op != pigeonhttp.OpHelloOK {
		t.Fatalf("hello-ok: %+v err=%v", ok, err)
	}

	if err := pigeonhttp.SendFrame(ctx, pair.A, pigeonhttp.Frame{
		Op:     pigeonhttp.OpHTTP,
		ID:     "root",
		Method: "GET",
		Path:   "/",
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := pigeonhttp.RecvFrame(ctx, pair.A)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Op != pigeonhttp.OpHTTPResp || resp.ID != "root" || resp.Status != 200 {
		t.Fatalf("root resp %+v", resp)
	}
	body, _ := resp.Body()
	if !strings.Contains(string(body), "react-cockpit") {
		t.Fatalf("body %q", body)
	}

	if err := pigeonhttp.SendFrame(ctx, pair.A, pigeonhttp.Frame{
		Op:     pigeonhttp.OpHTTP,
		ID:     "health",
		Method: "GET",
		Path:   "/api/health",
	}); err != nil {
		t.Fatal(err)
	}
	resp, err = pigeonhttp.RecvFrame(ctx, pair.A)
	if err != nil || resp.Status != 200 {
		t.Fatalf("health %+v err=%v", resp, err)
	}
	body, _ = resp.Body()
	if !strings.Contains(string(body), `"ok"`) {
		t.Fatalf("health body %q", body)
	}

	if err := pigeonhttp.SendFrame(ctx, pair.A, pigeonhttp.Frame{
		Op:   pigeonhttp.OpWSOpen,
		ID:   "mux1",
		Path: "/ws/mux",
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	var sawOpen, sawHello, sawEcho bool
	for time.Now().Before(deadline) && !(sawOpen && sawHello && sawEcho) {
		ctxR, cancelR := context.WithTimeout(ctx, 500*time.Millisecond)
		fr, err := pigeonhttp.RecvFrame(ctxR, pair.A)
		cancelR()
		if err != nil {
			continue
		}
		switch fr.Op {
		case pigeonhttp.OpWSOpenOK:
			if fr.ID == "mux1" {
				sawOpen = true
				_ = pigeonhttp.SendFrame(ctx, pair.A, pigeonhttp.Frame{
					Op:       pigeonhttp.OpWSMsg,
					ID:       "mux1",
					ChunkB64: pigeonhttp.WithBody([]byte(`{"ping":1}`)),
					Text:     true,
				})
			}
		case pigeonhttp.OpWSMsg:
			chunk, _ := fr.Chunk()
			if strings.Contains(string(chunk), "transcript:jevons") {
				sawHello = true
			}
			if strings.Contains(string(chunk), `"echo"`) {
				sawEcho = true
			}
		case pigeonhttp.OpWSOpenErr:
			t.Fatalf("ws open err: %s", fr.Error)
		}
	}
	if !sawOpen || !sawHello || !sawEcho {
		t.Fatalf("ws progress open=%v hello=%v echo=%v", sawOpen, sawHello, sawEcho)
	}

	_ = pigeonhttp.SendFrame(ctx, pair.A, pigeonhttp.Frame{Op: pigeonhttp.OpWSClose, ID: "mux1"})
	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
	}
}

func TestT628HelloRequired(t *testing.T) {
	pair := pigeonhttp.NewPipePair()
	srv := &pigeonhttp.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx, pair.B) }()

	_ = pigeonhttp.SendFrame(ctx, pair.A, pigeonhttp.Frame{Op: pigeonhttp.OpHTTP, ID: "x", Method: "GET", Path: "/"})
	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "hello") {
			t.Fatalf("err=%v", err)
		}
	case <-ctx.Done():
		t.Fatal("timeout")
	}
}
