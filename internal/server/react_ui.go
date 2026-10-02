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

	"github.com/marcelocantos/jevons/internal/buildident"
	"github.com/marcelocantos/jevons/ui"
)

// BuildMetaName is the <meta name> under which the served index.html carries
// the build id of the binary that served it (🎯T993). The cockpit reads it as
// the build it was loaded with, and reloads when a mux hello names another.
const BuildMetaName = "jevons-build"

// stampBuildMeta inserts the build meta tag right after <head>. An index
// without <head>, or an unknown ("") build, is served unchanged: the client
// then adopts the first hello's id as its baseline rather than guessing.
func stampBuildMeta(index []byte, buildID string) []byte {
	if buildID == "" {
		return index
	}
	i := bytes.Index(index, []byte("<head>"))
	if i < 0 {
		return index
	}
	i += len("<head>")
	tag := fmt.Sprintf(`<meta name=%q content=%q>`, BuildMetaName, html.EscapeString(buildID))
	out := make([]byte, 0, len(index)+len(tag))
	out = append(out, index[:i]...)
	out = append(out, tag...)
	out = append(out, index[i:]...)
	return out
}

// RegisterProductUIRoutes serves the same embedded React build for development,
// isolates and released binaries. Vite is a separate, opt-in editing tool.
func RegisterProductUIRoutes(mux *http.ServeMux) error {
	files, err := ui.Files()
	if err != nil {
		return fmt.Errorf("open React bundle: %w", err)
	}
	return RegisterReactUIRoutes(mux, files)
}

// RegisterReactUIRoutes serves files with the running binary's build id
// stamped into the document (🎯T993).
func RegisterReactUIRoutes(mux *http.ServeMux, files fs.FS) error {
	return registerReactUIRoutes(mux, files, buildident.Binary())
}

func noCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

// RegisterReactUIRoutes validates the document's local assets before serving.
// API routes registered on the mux remain authoritative; missing assets never
// fall back to HTML. The immutable bundle cannot catch a half-written build.
func registerReactUIRoutes(mux *http.ServeMux, files fs.FS, buildID string) error {
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return fmt.Errorf("React index: %w", err)
	}
	index = stampBuildMeta(index, buildID)
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
		// The document is the stamped copy (🎯T993), whichever path names it.
		body, err := index, error(nil)
		if name != "index.html" {
			body, err = fs.ReadFile(files, name)
		}
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
