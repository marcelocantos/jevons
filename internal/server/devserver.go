// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/fsnotify/fsnotify"

	"github.com/marcelocantos/jevons/web"
)

// devReloadDebounce coalesces fsnotify bursts (multi-file agent edits)
// into one client reload (🎯T144).
const devReloadDebounce = 400 * time.Millisecond

// DevServer serves static files from a directory and notifies
// connected clients to reload when files change.
type DevServer struct {
	dir     string
	handler http.Handler

	mu            sync.Mutex
	reloadChs     []chan struct{}
	reloadTimer   *time.Timer // 🎯T144 debounce
}

// NewDevServer creates a dev server for the given directory.
func NewDevServer(dir string) *DevServer {
	return &DevServer{
		dir:     dir,
		handler: http.FileServer(http.Dir(dir)),
	}
}

// RegisterUIRoutes serves the web UI: from dir (with hot reload) when it
// exists on disk, else from the embedded copy — a released binary must
// serve the UI standalone (🎯T53; brew installs ship no repo checkout).
// Returns the DevServer when disk mode is active, nil in embedded mode.
func RegisterUIRoutes(mux *http.ServeMux, dir string) *DevServer {
	if st, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !st.IsDir() {
		ds := NewDevServer(dir)
		ds.RegisterRoutes(mux)
		return ds
	}
	slog.Info("serving embedded web UI (no on-disk web/)", "checked", dir)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		raw, err := web.FS.ReadFile("index.html")
		if err != nil {
			slog.Error("embedded index.html unreadable", "err", err)
			http.Error(w, "embedded index.html unreadable", http.StatusInternalServerError)
			return
		}
		// An embedded tree is frozen at build time, so nothing in it can be
		// mid-write — the 🎯T375 settle pass has no work to do here.
		serveGuardedIndex(w, raw, "embedded", func(ref string) bool {
			_, err := fs.Stat(web.FS, ref)
			return err == nil
		}, nil)
	})
	scriptsFS, err := fs.Sub(web.FS, "scripts")
	if err != nil {
		slog.Error("embedded scripts missing", "err", err)
		return nil
	}
	scripts := http.StripPrefix("/scripts/", http.FileServerFS(scriptsFS))
	mux.Handle("GET /scripts/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		scripts.ServeHTTP(w, r)
	}))
	return nil
}

// RegisterRoutes adds the static file server and reload WebSocket to the mux.
// Must be called before any other routes are registered — the file server
// acts as the fallback for unmatched paths.
func (ds *DevServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /ws/reload", ds.handleReload)

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		raw, err := os.ReadFile(filepath.Join(ds.dir, "index.html"))
		if err != nil {
			// Loud: the daily cockpit has no index to serve. Silence here
			// is what 🎯T374 is about.
			slog.Error("cockpit index.html unreadable", "dir", ds.dir, "err", err)
			http.Error(w, "cockpit index.html unreadable: "+err.Error(),
				http.StatusInternalServerError)
			return
		}
		// 🎯T375: N fleet workers write into this same tree, so the read
		// above may have caught a file mid-write. Wait for the reference set
		// to hold still; if it settles, re-read so the bytes we serve are the
		// settled ones rather than the ones we happened to catch.
		moving := settleServingTree(ds.dir, web.IndexScriptRefs(raw))
		if len(moving) == 0 {
			if settled, err := os.ReadFile(filepath.Join(ds.dir, "index.html")); err == nil {
				raw = settled
			}
		}
		serveGuardedIndex(w, raw, ds.dir, func(ref string) bool {
			st, err := os.Stat(filepath.Join(ds.dir, filepath.FromSlash(ref)))
			return err == nil && !st.IsDir()
		}, moving)
	})
	// Serve static assets under /scripts/. no-cache on every response so a
	// jevonsd restart with new assets is picked up by the next page load
	// instead of being served stale from the browser cache.
	scripts := http.StripPrefix("/scripts/",
		http.FileServer(http.Dir(filepath.Join(ds.dir, "scripts"))))
	mux.Handle("GET /scripts/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		scripts.ServeHTTP(w, r)
	}))
}

func noCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

// Watch starts watching the directory for changes and triggers reloads.
// Blocks until the watcher is closed.
// Set JEVONS_DEV_RELOAD=0 to disable reload notifications (still serves disk).
func (ds *DevServer) Watch() error {
	if os.Getenv("JEVONS_DEV_RELOAD") == "0" {
		slog.Info("dev server reload disabled", "env", "JEVONS_DEV_RELOAD=0", "dir", ds.dir)
		return nil
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	if err := watcher.Add(ds.dir); err != nil {
		return err
	}
	slog.Info("dev server watching", "dir", ds.dir, "debounce", devReloadDebounce.String())

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				slog.Info("file changed, scheduling reload", "file", event.Name)
				ds.scheduleReload()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			slog.Error("watcher error", "err", err)
		}
	}
}

// scheduleReload debounces triggerReload (🎯T144).
func (ds *DevServer) scheduleReload() {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if ds.reloadTimer != nil {
		ds.reloadTimer.Stop()
	}
	ds.reloadTimer = time.AfterFunc(devReloadDebounce, func() {
		ds.triggerReload()
	})
}

func (ds *DevServer) handleReload(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, wsAcceptOptions())
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ch := make(chan struct{}, 1)
	ds.mu.Lock()
	ds.reloadChs = append(ds.reloadChs, ch)
	ds.mu.Unlock()

	defer func() {
		ds.mu.Lock()
		for i, c := range ds.reloadChs {
			if c == ch {
				ds.reloadChs = append(ds.reloadChs[:i], ds.reloadChs[i+1:]...)
				break
			}
		}
		ds.mu.Unlock()
	}()

	ctx := r.Context()
	select {
	case <-ctx.Done():
	case <-ch:
		conn.Write(ctx, websocket.MessageText, []byte("reload"))
	}
}

func (ds *DevServer) triggerReload() {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	for _, ch := range ds.reloadChs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	// Clear — each reload signal is consumed once, new connections
	// get the next one.
	ds.reloadChs = nil
}
