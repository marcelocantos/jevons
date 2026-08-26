// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package uidaemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlistXMLNamesBothJobs(t *testing.T) {
	spec := Spec{
		Binary:   "/repo/bin/jevonsd",
		Home:     "/Users/x",
		StateDir: "/Users/x/.jevons",
		PathEnv:  "/usr/bin:/bin:/opt/homebrew/bin",
		Upstream: "127.0.0.1:13705",
	}
	react := ReactPlistXML(spec)
	for _, want := range []string{
		ReactLabel,
		"-ui",
		"probe",
		"<key>StartInterval</key>",
		"/Users/x/.jevons/ui-react-probe.log",
		"/opt/homebrew/bin",
	} {
		if !strings.Contains(react, want) {
			t.Errorf("React plist missing %q:\n%s", want, react)
		}
	}
	if strings.Contains(react, "KeepAlive") {
		t.Error("React plist must not KeepAlive jevonsd")
	}
	if strings.Contains(react, "npm") || strings.Contains(react, "5173") {
		t.Error("React plist must not be the Vite :5173 agent")
	}

	vanilla := VanillaPlistXML(spec)
	for _, want := range []string{
		VanillaLabel,
		"-ui",
		"vanilla",
		"13706",
		"127.0.0.1:13705",
		"<key>KeepAlive</key>",
		"/Users/x/.jevons/ui-vanilla.log",
	} {
		if !strings.Contains(vanilla, want) {
			t.Errorf("vanilla plist missing %q:\n%s", want, vanilla)
		}
	}
	if strings.Contains(vanilla, "npm") || strings.Contains(vanilla, "5173") {
		t.Error("vanilla plist must not be the Vite :5173 agent")
	}
}

func TestLooksLikeReactDocument(t *testing.T) {
	if !LooksLikeReactDocument(`<!doctype html><div id="root"></div>`) {
		t.Fatal("React #root must pass")
	}
	if LooksLikeReactDocument(`<!-- DEPRECATED REFERENCE --> <div id="root">`) {
		t.Fatal("vanilla banner must fail even if a root id is present")
	}
	if LooksLikeReactDocument(`<html><body>vanilla</body></html>`) {
		t.Fatal("vanilla-without-root must fail")
	}
}

func TestProbeReact(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<div id="root"></div>`))
	}))
	defer ok.Close()
	got := ProbeReact(ok.Client(), ok.URL+"/")
	if !got.OK {
		t.Fatalf("React document: %+v", got)
	}

	vanilla := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!-- DEPRECATED REFERENCE -->`))
	}))
	defer vanilla.Close()
	got = ProbeReact(vanilla.Client(), vanilla.URL+"/")
	if got.OK || !strings.Contains(got.Reason, "not the React") {
		t.Fatalf("vanilla document: %+v", got)
	}
}
