// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
)

// ReactDistReady reports whether dir looks like a Vite `ui/dist` tree
// (index.html present). Dist is not go:embed (🎯T360); a missing tree
// is a build step, not a silent vanilla fallback on daily :13705.
func ReactDistReady(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "index.html"))
	return err == nil && !st.IsDir()
}

// RegisterReactUIRoutes serves the Vite React build from dist.
// GET /{$} is the SPA document; /assets/ is the hashed bundle.
func RegisterReactUIRoutes(mux *http.ServeMux, dist string) {
	slog.Info("serving React cockpit from ui/dist (🎯T540.2)", "dir", dist)
	index := filepath.Join(dist, "index.html")
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		http.ServeFile(w, r, index)
	})
	files := http.FileServer(http.Dir(dist))
	mux.Handle("GET /assets/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		files.ServeHTTP(w, r)
	}))
	// Named root assets only — GET /{file} conflicts with /mcp (Go ServeMux).
	mux.HandleFunc("GET /favicon.svg", func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(dist, "favicon.svg")
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			http.NotFound(w, r)
			return
		}
		noCache(w)
		http.ServeFile(w, r, p)
	})
}

// RegisterProductUIRoutes is the 🎯T540.2 product GET /:
// React from ui/dist when the build exists. Daily (requireReact) fails
// closed without dist — it must not silently serve vanilla web/. Isolates
// without dist keep vanilla (named dual-path residual; journeys then use
// the Vite proxy).
func RegisterProductUIRoutes(mux *http.ServeMux, reactDir, webDir string, requireReact bool) (*DevServer, error) {
	if ReactDistReady(reactDir) {
		RegisterReactUIRoutes(mux, reactDir)
		return nil, nil
	}
	if requireReact {
		return nil, fmt.Errorf("daily GET / requires React build at %s (🎯T540.2); run make ui-build", reactDir)
	}
	slog.Info("isolate GET / has no ui/dist; serving vanilla (🎯T540.2 dual-path residual)",
		"checked", reactDir)
	return RegisterUIRoutes(mux, webDir), nil
}
