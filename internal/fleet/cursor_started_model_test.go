// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestCursorCLIDefaultModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := cursorCLIDefaultModel(); got != "" {
		t.Fatalf("missing config model=%q", got)
	}
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"model":{"modelId":"composer-2.5"},"selectedModel":{"modelId":"claude-opus-5"}}`)
	if err := os.WriteFile(filepath.Join(dir, "cli-config.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := cursorCLIDefaultModel(); got != "composer-2.5" {
		t.Fatalf("model=%q want model.modelId, not selectedModel", got)
	}
}

func TestRecordUnpinnedCursorStartDoesNotPin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cli-config.json"), []byte(`{"model":{"modelId":"composer-2.5"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const sid = "sess-unpinned"
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", SessionID: sid, Provider: claudia.ProviderCursor, ConnectPID: 4242,
	}); err != nil {
		t.Fatal(err)
	}
	look := LookCursorStart(reg, "jevons-po")
	if !look.applicable || look.model != "composer-2.5" || look.wasAlive {
		t.Fatalf("look=%+v", look)
	}
	look.Record(reg, "jevons-po")
	def := reg.Def("jevons-po")
	if def == nil || def.Model != "" {
		t.Fatalf("model=%q; recording pinned the seat", def.Model)
	}
	model, pid, ok := CursorStartedModel(sid)
	if !ok || model != "composer-2.5" || pid != 4242 {
		t.Fatalf("recorded model=%q pid=%d ok=%v", model, pid, ok)
	}
	written, err := os.ReadFile(filepath.Join(home, ".jevons", "started-model.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), `"composer-2.5"`) || !strings.Contains(string(written), "4242") {
		t.Fatalf("started-model.json = %s", written)
	}
	if reg.Def("jevons-po").Model != "" {
		t.Fatal("write set AgentDef.Model")
	}

	// A pin is the argv model. Do not also snapshot the CLI file.
	pinned := *def
	pinned.Model = "claude-opus-5"
	if err := reg.Register(pinned); err != nil {
		t.Fatal(err)
	}
	if got := LookCursorStart(reg, "jevons-po"); got.applicable {
		t.Fatal("pinned seat was treated as unpinned")
	}

	// Same process still running, pin still empty: do not rewrite from today's file.
	cleared := *reg.Def("jevons-po")
	cleared.Model = ""
	if err := reg.Register(cleared); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cli-config.json"), []byte(`{"model":{"modelId":"claude-opus-5"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	again := CursorStartLook{applicable: true, wasAlive: true, beforePID: 4242, model: "claude-opus-5"}
	again.Record(reg, "jevons-po")
	model, pid, ok = CursorStartedModel(sid)
	if !ok || model != "composer-2.5" || pid != 4242 {
		t.Fatalf("adopt rewrote the record to model=%q pid=%d", model, pid)
	}
}
