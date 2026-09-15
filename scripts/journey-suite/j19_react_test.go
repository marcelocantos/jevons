// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
)

func TestJ19HTMLIsVanilla(t *testing.T) {
	root, err := j19RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	// Tiny historical signatures, not a runnable copy of the old UI.
	for _, old := range []string{"DEPRECATED REFERENCE", `<script src="boot_sentinel.js"></script>`} {
		if !j19HTMLIsVanilla([]byte(old)) {
			t.Fatal("legacy root accepted")
		}
	}
	ui, err := os.ReadFile(filepath.Join(root, "ui", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if j19HTMLIsVanilla(ui) {
		t.Fatal("ui/index.html must classify as React (#root)")
	}
	if j19HTMLIsVanilla([]byte(`<div id="root"></div>`)) {
		t.Fatal("bare #root is React, not vanilla")
	}
	if !j19HTMLIsVanilla([]byte(`<html><body>no mount</body></html>`)) {
		t.Fatal("unknown HTML must fail closed")
	}
}

func TestJ19RefuseDailyHost(t *testing.T) {
	if err := refuseDailyHost("127.0.0.1:13705"); err == nil {
		t.Fatal("react load path must RefuseDaily :13705")
	}
	if err := refuseDailyHost("127.0.0.1:13715"); err != nil {
		t.Fatalf("isolate default: %v", err)
	}
	if err := portguard.RefuseDaily(13705); err == nil {
		t.Fatal("portguard.RefuseDaily(13705) must error")
	}
}

func TestPackagedReactSurfaceNeverFallsBack(t *testing.T) {
	for _, body := range []string{`<div id="root"></div>`, `<html>not React</html>`, `DEPRECATED REFERENCE`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			u, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			port, err := strconv.Atoi(u.Port())
			if err != nil {
				t.Fatal(err)
			}
			s := suite{host: u.Host, port: port}
			surface, err := s.startJ19ReactSurface()
			if j19HTMLIsVanilla([]byte(body)) {
				if err == nil || surface != nil {
					t.Fatal("bad packaged root must fail, never start a substitute server")
				}
			} else if err != nil || surface.host != u.Host || surface.via != "isolate" {
				t.Fatalf("canonical surface: %v, %v", surface, err)
			}
		})
	}
}

func TestJ19PaintTargetsReactNotVanillaGlobals(t *testing.T) {
	root, err := j19RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "scripts", "journey-suite", "j19_paint.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)
	if !strings.Contains(src, "getElementById('root')") {
		t.Error("j19_paint.js must wait for React #root")
	}
	if strings.Contains(src, "__transcriptRows") {
		t.Error("j19_paint.js must not wait on vanilla __transcriptRows")
	}
	if strings.Contains(src, "viewport_census.js") {
		t.Error("j19_paint.js must not import vanilla viewport_census.js")
	}
	if !strings.Contains(src, "refuses development port") {
		t.Error("j19_paint.js must still refuse :13705")
	}
	if !strings.Contains(src, "T540.2") {
		t.Error("j19_paint.js must name the T540.2 residual")
	}
}
