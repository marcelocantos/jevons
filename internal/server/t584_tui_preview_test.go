// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/chatlog"
)

func t584Fixture(t *testing.T) (*Server, string, func() []string, func() []string) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "chat.jsonl")
	clog, err := chatlog.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	s := New("test", dir)
	s.mu.Lock()
	s.chatLog = clog
	s.mu.Unlock()
	ch := make(chan string, 64)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()
	drain := func() []string {
		var out []string
		for {
			select {
			case line := <-ch:
				out = append(out, line)
			default:
				return out
			}
		}
	}
	drainProv := func() []string {
		var out []string
		for _, line := range drain() {
			if parseWireType(line) == wireTypeProvisionalAssistant ||
				parseWireType(line) == wireTypeTUIPreviewFault {
				out = append(out, line)
			}
		}
		return out
	}
	return s, logPath, drain, drainProv
}

func parseWireType(line string) string {
	var m struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal([]byte(line), &m)
	return m.Type
}

func provisionalText(line string) string {
	var m struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal([]byte(line), &m)
	return m.Text
}

func TestT584PreviewFirstLiveNotJournaled(t *testing.T) {
	s, logPath, _, drainProv := t584Fixture(t)
	s.DeliverOverseerEvent(claudia.Event{
		Type: "progress", ProgressType: claudia.ProgressTUIPreview,
		MessageID: "tui-preview-1", PreviewUpdate: claudia.PreviewUpdateRewrite,
		Text: "hello pane",
	})
	lines := drainProv()
	if len(lines) != 1 {
		t.Fatalf("lines=%d want 1 provisional fan-out: %v", len(lines), lines)
	}
	if parseWireType(lines[0]) != wireTypeProvisionalAssistant {
		t.Fatalf("type=%q", parseWireType(lines[0]))
	}
	if !strings.Contains(provisionalText(lines[0]), "hello pane") {
		t.Fatalf("text=%q", provisionalText(lines[0]))
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hello pane") {
		t.Fatalf("preview leaked to journal: %s", raw)
	}
}

func TestT584PreviewThenSealShrinksTail(t *testing.T) {
	s, logPath, _, drainProv := t584Fixture(t)
	s.DeliverOverseerEvent(claudia.Event{
		Type: "progress", ProgressType: claudia.ProgressTUIPreview,
		MessageID: "tui-preview-1", PreviewUpdate: claudia.PreviewUpdateRewrite,
		Text: "draft one",
	})
	drainProv()
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "**sealed** markdown"})
	prov := drainProv()
	if len(prov) != 1 || provisionalText(prov[0]) != "" {
		t.Fatalf("provisional clear=%v", prov)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "sealed") {
		t.Fatalf("assistant seal not journaled: %s", raw)
	}
}

func TestT584TwoBlockPreviewThenPartialSeal(t *testing.T) {
	s, _, _, drainProv := t584Fixture(t)
	s.DeliverOverseerEvent(claudia.Event{
		Type: "progress", ProgressType: claudia.ProgressTUIPreview,
		MessageID: "tui-preview-1", PreviewUpdate: claudia.PreviewUpdateRewrite,
		Text: "block A",
	})
	drainProv()
	s.DeliverOverseerEvent(claudia.Event{
		Type: "progress", ProgressType: claudia.ProgressTUIPreview,
		MessageID: "tui-preview-2", PreviewUpdate: claudia.PreviewUpdateRewrite,
		Text: "block B",
	})
	drainProv()
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "sealed A"})
	lines := drainProv()
	var prov string
	for _, line := range lines {
		if parseWireType(line) == wireTypeProvisionalAssistant {
			prov = provisionalText(line)
		}
	}
	if !strings.Contains(prov, "block B") {
		t.Fatalf("tail after first seal=%q", prov)
	}
	if strings.Contains(prov, "block A") {
		t.Fatalf("sealed block still in tail=%q", prov)
	}
}

func TestT584RestartLosesPreview(t *testing.T) {
	s, _, _, _ := t584Fixture(t)
	s.DeliverOverseerEvent(claudia.Event{
		Type: "progress", ProgressType: claudia.ProgressTUIPreview,
		MessageID: "tui-preview-1", PreviewUpdate: claudia.PreviewUpdateRewrite,
		Text: "ephemeral",
	})
	s2 := New("test", t.TempDir())
	s2.mu.Lock()
	s2.overseerStreamID = "new-stream"
	s2.mu.Unlock()
	if suffix := s2.overseerTUIPreview.fencedSuffix(); suffix != "" {
		t.Fatalf("new server kept preview=%q", suffix)
	}
}

func TestT584PreviewFaultSurfacesOnce(t *testing.T) {
	s, _, _, drainProv := t584Fixture(t)
	ev := claudia.Event{
		Type: "progress", ProgressType: claudia.ProgressTUIPreviewFault,
		MessageID: "tui-preview-fault:ran_shell_chrome_form",
		Text:      "tui_preview_fault: invariant=ran_shell_chrome_form",
	}
	s.DeliverOverseerEvent(ev)
	s.DeliverOverseerEvent(ev)
	lines := drainProv()
	faults := len(lines)
	if faults != 1 {
		t.Fatalf("fault lines=%d want 1", faults)
	}
}

func TestT584PreviewNotLosslessOnWireDrop(t *testing.T) {
	_, ok := chatWireLine(claudia.Event{
		Type: "progress", ProgressType: claudia.ProgressTUIPreview, Text: "x",
	})
	if ok {
		t.Fatal("chatWireLine must not map tui_preview")
	}
}
