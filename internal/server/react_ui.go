// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/marcelocantos/jevons/ui"
)

// RegisterProductUIRoutes serves the same embedded React build for development,
// isolates and released binaries. Vite is a separate, opt-in editing tool.
func RegisterProductUIRoutes(mux *http.ServeMux) error {
	files, err := ui.Files()
	if err != nil {
		return fmt.Errorf("open React bundle: %w", err)
	}
	return RegisterReactUIRoutes(mux, files)
}

func noCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

// RegisterReactUIRoutes validates the document's local assets before serving.
// API routes registered on the mux remain authoritative; missing assets never
// fall back to HTML. The immutable bundle cannot catch a half-written build.
func RegisterReactUIRoutes(mux *http.ServeMux, files fs.FS) error {
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return fmt.Errorf("React index: %w", err)
	}
	if !bytes.Contains(index, []byte(`id="root"`)) {
		return fmt.Errorf("React bundle has no root mount")
	}
	parser := html.NewTokenizer(bytes.NewReader(index))
	for {
		kind := parser.Next()
		if kind == html.ErrorToken {
			if parser.Err() != io.EOF {
				return fmt.Errorf("React index: %w", parser.Err())
			}
			break
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		token := parser.Token()
		if token.Data != "script" && token.Data != "link" && token.Data != "img" {
			continue
		}
		for _, attr := range token.Attr {
			if attr.Key != "src" && attr.Key != "href" {
				continue
			}
			ref, err := url.Parse(attr.Val)
			if err != nil {
				return fmt.Errorf("React asset URL %q: %w", attr.Val, err)
			}
			if ref.IsAbs() || ref.Host != "" || ref.Path == "" {
				continue
			}
			name := strings.TrimPrefix(ref.Path, "/")
			if !fs.ValidPath(name) {
				return fmt.Errorf("invalid React asset path %q", name)
			}
			if info, err := fs.Stat(files, name); err != nil || info.IsDir() {
				return fmt.Errorf("React bundle missing asset %q", name)
			}
		}
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		namespace, _, _ := strings.Cut(name, "/")
		if !fs.ValidPath(name) || namespace == "api" || namespace == "ws" || namespace == "mcp" || namespace == "health" {
			http.NotFound(w, r)
			return
		}
		body, err := fs.ReadFile(files, name)
		if err != nil {
			// Only navigation requests get the SPA document. A missing bundle
			// chunk must be a 404, not a successful response containing HTML.
			if !strings.Contains(r.Header.Get("Accept"), "text/html") || path.Ext(name) != "" || strings.HasPrefix(name, "assets/") {
				http.NotFound(w, r)
				return
			}
			name, body = "index.html", index
		}
		noCache(w)
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
	})
	return nil
}
