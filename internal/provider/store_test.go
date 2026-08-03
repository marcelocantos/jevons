// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/provider"
)

// 🎯T27.2 oracle: persistence home opens, is additive, stores desired + cursors + manifests.

func TestStoreOpenCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "providers", "registry.db")
	s, err := provider.OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer s.Close()
	if s.Path() != path {
		t.Fatalf("path=%q", s.Path())
	}
}

func TestStoreReplaceDesiredAndList(t *testing.T) {
	s, err := provider.OpenStore(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	enFalse := false
	decls := []config.ProviderDecl{
		{
			ID: "mnemo", Transport: config.ProviderTransportConnect,
			Params: map[string]any{"url": "http://127.0.0.1:8741", "x": "y"},
		},
		{
			ID: "pulse", Transport: config.ProviderTransportLaunch,
			Enable: &enFalse,
			Params: map[string]any{"exec": "/bin/pulse", "argv": []any{"--a"}},
		},
	}
	if err := s.ReplaceDesired(decls); err != nil {
		t.Fatalf("ReplaceDesired: %v", err)
	}
	got, err := s.ListDesired()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	byID := map[string]provider.DesiredRow{}
	for _, r := range got {
		byID[r.ID] = r
	}
	mnemo := byID["mnemo"]
	if !mnemo.Enable || mnemo.Transport != config.ProviderTransportConnect {
		t.Fatalf("mnemo=%+v", mnemo)
	}
	if mnemo.Params["url"] != "http://127.0.0.1:8741" || mnemo.Params["x"] != "y" {
		t.Fatalf("params=%v", mnemo.Params)
	}
	if byID["pulse"].Enable {
		t.Fatal("pulse should be disabled")
	}

	// Replace empties unknown rows (full snapshot).
	if err := s.ReplaceDesired(nil); err != nil {
		t.Fatal(err)
	}
	got, err = s.ListDesired()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("after empty replace: %+v", got)
	}
}

func TestStoreManifestAndCursors(t *testing.T) {
	s, err := provider.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	mock := provider.NewMock()
	m := mock.Describe()
	if err := s.PutManifest(m); err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
	got, ok, err := s.GetManifest(provider.MockID)
	if err != nil || !ok {
		t.Fatalf("GetManifest: ok=%v err=%v", ok, err)
	}
	if got.ID != m.ID || got.Version != m.Version {
		t.Fatalf("got=%+v", got)
	}
	if _, ok, err := s.GetManifest("nope"); err != nil || ok {
		t.Fatalf("missing should be ok=false: ok=%v err=%v", ok, err)
	}

	if err := s.SetCursor(provider.MockID, provider.MockFeedName, 42); err != nil {
		t.Fatal(err)
	}
	seq, err := s.GetCursor(provider.MockID, provider.MockFeedName)
	if err != nil || seq != 42 {
		t.Fatalf("cursor=%d err=%v", seq, err)
	}
	// Upsert bumps seq.
	if err := s.SetCursor(provider.MockID, provider.MockFeedName, 99); err != nil {
		t.Fatal(err)
	}
	seq, _ = s.GetCursor(provider.MockID, provider.MockFeedName)
	if seq != 99 {
		t.Fatalf("seq=%d", seq)
	}
	all, err := s.ListCursors(provider.MockID)
	if err != nil || all[provider.MockFeedName] != 99 {
		t.Fatalf("list=%v err=%v", all, err)
	}
}

func TestStoreReopenPreservesDesired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	s1, err := provider.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.ReplaceDesired([]config.ProviderDecl{{
		ID: "a", Transport: config.ProviderTransportConnect,
		Params: map[string]any{"url": "http://x"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := provider.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err := s2.ListDesired()
	if err != nil || len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("reopen: %+v err=%v", got, err)
	}
}

func TestDefaultStorePath(t *testing.T) {
	p := provider.DefaultStorePath("/tmp/state")
	if p != filepath.Join("/tmp/state", "providers", "registry.db") {
		t.Fatalf("path=%q", p)
	}
}
