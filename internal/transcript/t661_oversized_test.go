// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package transcript

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/discovery"
)

// 🎯T661 fixture: the redacted shape of jv-t657-steer-ui session 78239742…230bc6
// (2026-09-15), where a screenshot tool_result stored a ~600 KB base64 PNG
// twice on one line. The template keeps the record structure and replaces the
// image data with <FILL>; the test inflates it so the line is over 1.5 MiB,
// past both this reader's old 1 MiB scanner cap and the broker line limit.
const t661TemplatePath = "testdata/t661_oversized_template.jsonl"

const t661FillBytes = 800_000 // twice per line → ~1.6 MB

func t661Session(t *testing.T, fill string) (*Reader, string, string) {
	t.Helper()
	tpl, err := os.ReadFile(t661TemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ReplaceAll(string(tpl), "<FILL>", fill)
	projects := filepath.Join(t.TempDir(), "projects")
	sid := "00000000-0000-4000-8000-00000000c6b6"
	dir := filepath.Join(projects, discovery.EncodeClaudeProject("/tmp/t661"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, sid+".jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewReaderRoots(discovery.Roots{ClaudeProjects: projects}), sid, path
}

func TestT661_ReadRendersTurnsAroundAnOversizedLine(t *testing.T) {
	r, sid, path := t661Session(t, strings.Repeat("A", t661FillBytes))

	raw, _ := os.ReadFile(path)
	line3 := bytes.Split(raw, []byte("\n"))[2]
	if len(line3) <= 3*BrokerLineLimit/2 {
		t.Fatalf("fixture line 3 is %d bytes; the regression needs > 1.5 MiB", len(line3))
	}

	turns, err := r.Read(sid)
	if err != nil {
		t.Fatalf("an oversized line must not make the session unreadable: %v", err)
	}
	if len(turns) != 3 {
		t.Fatalf("want marker + user + assistant, got %d: %+v", len(turns), turns)
	}
	if role, _ := turns[0]["role"].(string); role != MarkerRole {
		t.Fatalf("first entry must be the oversized marker, got %+v", turns[0])
	}
	if _, hasNumber := turns[0]["turn_number"]; hasNumber {
		t.Fatalf("the marker is a note, not a turn: %+v", turns[0])
	}
	marker, _ := turns[0]["text"].(string)
	for _, want := range []string{"line 3 (", "1048576-byte", "jevons_agent_kill", "🎯T661"} {
		if !strings.Contains(marker, want) {
			t.Fatalf("marker must carry %q: %s", want, marker)
		}
	}
	if text, _ := turns[1]["text"].(string); text != "take a screenshot of the cockpit" {
		t.Fatalf("user turn: %+v", turns[1])
	}
	if text, _ := turns[2]["text"].(string); !strings.Contains(text, "Latest visible") {
		t.Fatalf("assistant turn after the oversized line must render: %+v", turns[2])
	}
}

func TestT661_TailAndScanSeeThroughAnOversizedLine(t *testing.T) {
	r, sid, path := t661Session(t, strings.Repeat("A", t661FillBytes))

	entries, err := r.Tail(sid, 0)
	if err != nil {
		t.Fatalf("Tail must read past the oversized line: %v", err)
	}
	if len(entries) == 0 || !strings.Contains(entries[len(entries)-1].Text, "Latest visible") {
		t.Fatalf("last entry must be the reply after the oversized line: %+v", entries)
	}

	got, err := ScanOversized(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	want := len(bytes.Split(raw, []byte("\n"))[2])
	if len(got) != 1 || got[0].Line != 3 || got[0].Bytes != want {
		t.Fatalf("census: got %+v, want one entry line=3 bytes=%d", got, want)
	}
	if OversizedMarker(nil) != "" {
		t.Fatal("no oversized lines means no marker")
	}
}

func TestT661_TruncatePreservesAnOversizedLineVerbatim(t *testing.T) {
	r, sid, path := t661Session(t, strings.Repeat("A", t661FillBytes))
	before, _ := os.ReadFile(path)
	if err := r.Truncate(sid, 10); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatalf("truncate that keeps every turn must rewrite the file byte-for-byte (before=%d after=%d)",
			len(before), len(after))
	}
}

func TestT661_NormalSessionCarriesNoMarker(t *testing.T) {
	r, sid, _ := t661Session(t, "iVBORw0KGgo=")
	turns, err := r.Read(sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 || turns[0]["role"] != "user" {
		t.Fatalf("a normal session has no marker: %+v", turns)
	}
}
