// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
)

func TestJ19HTMLIsVanilla(t *testing.T) {
	root, err := j19RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	web, err := os.ReadFile(filepath.Join(root, "web", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !j19HTMLIsVanilla(web) {
		t.Fatal("web/index.html must classify as vanilla (residual until T540.2)")
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
		t.Fatal("unknown HTML fails closed as vanilla so J19 uses the Vite proxy")
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

func TestJ19ViteConfigExistsAndRefusesDaily(t *testing.T) {
	cfg, err := j19ViteConfig()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)
	if !strings.Contains(src, "5173") {
		t.Error("vite config must name the :5173-style proxy")
	}
	if !strings.Contains(src, "13705") {
		t.Error("vite config must refuse daily :13705")
	}
	if !strings.Contains(src, "T540.2") {
		t.Error("vite config must name the vanilla GET / residual (T540.2)")
	}
	if !strings.Contains(src, "J19_ISOLATE") {
		t.Error("vite config must proxy to the isolate, not a hardcoded daily port")
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
	if !strings.Contains(src, "refuses daily port") {
		t.Error("j19_paint.js must still refuse :13705")
	}
	if !strings.Contains(src, "T540.2") {
		t.Error("j19_paint.js must name the T540.2 residual")
	}
}
