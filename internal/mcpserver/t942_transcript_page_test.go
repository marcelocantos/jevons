// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// 🎯T942: jevons_transcript_read returns one bounded page. A whole transcript
// (5,000 turns here, 673 KB on 2026-09-30) never enters the caller's context
// in one call; the header says what was left out and how to page back.
func TestT942TranscriptReadReturnsABoundedPage(t *testing.T) {
	const total = 5000
	turns := make([]map[string]any, total)
	for i := range turns {
		turns[i] = map[string]any{"role": "user", "text": fmt.Sprintf("turn-%05d %s", i+1, strings.Repeat("x", 190))}
	}
	s := &Server{transcript: &TranscriptOps{
		GetID: func() string { return "sess" },
		Read:  func(string) ([]map[string]any, error) { return turns, nil },
	}}
	read := func(args map[string]any) string {
		t.Helper()
		req := mcp.CallToolRequest{}
		req.Params.Arguments = args
		res, err := s.handleTranscriptRead(context.Background(), req)
		if err != nil || res.IsError {
			t.Fatalf("read %v: %v %s", args, err, toolText(res))
		}
		return toolText(res)
	}

	page := read(map[string]any{})
	if len(page) > transcriptPageBytes+1024 {
		t.Fatalf("default page is %d bytes, budget %d", len(page), transcriptPageBytes)
	}
	if !strings.Contains(page, "turns=5000 showing 4961-5000") || !strings.Contains(page, "before=4961") {
		t.Fatalf("header does not mark the omission:\n%s", strings.SplitN(page, "\n", 2)[0])
	}
	if !strings.Contains(page, "turn-05000") || strings.Contains(page, "turn-04960") {
		t.Fatal("default page is not the newest 40 turns")
	}

	// Paging back walks the whole transcript without overlap or gaps.
	seen := map[int]bool{}
	next := regexp.MustCompile(`before=(\d+)`)
	args := map[string]any{"limit": float64(200)}
	for pages := 0; ; pages++ {
		p := read(args)
		if len(p) > transcriptPageBytes+1024 {
			t.Fatalf("page %d is %d bytes", pages, len(p))
		}
		for _, m := range regexp.MustCompile(`turn-(\d{5})`).FindAllStringSubmatch(p, -1) {
			n, _ := strconv.Atoi(m[1])
			if seen[n] {
				t.Fatalf("turn %d returned twice", n)
			}
			seen[n] = true
		}
		m := next.FindStringSubmatch(p)
		if m == nil {
			break
		}
		b, _ := strconv.Atoi(m[1])
		args = map[string]any{"limit": float64(200), "before": float64(b)}
		if pages > total {
			t.Fatal("paging never ended")
		}
	}
	if len(seen) != total {
		t.Fatalf("paging covered %d of %d turns", len(seen), total)
	}
}

// 🎯T942: other history-sized answers are bounded too.
func TestT942OtherHistorySizedAnswersAreBounded(t *testing.T) {
	long := strings.Repeat("r", 3*transcriptPageBytes)
	got := boundText(long, "ask for one part with section=<heading>")
	if len(got) > transcriptPageBytes+200 || !strings.Contains(got, "more bytes not shown") || !strings.Contains(got, "section=") {
		t.Fatalf("bounded text is %d bytes / %q…", len(got), got[len(got)-120:])
	}
	if boundText("short", "x") != "short" {
		t.Fatal("a short text was altered")
	}
	items := make([]int, 120)
	for i := range items {
		items[i] = i
	}
	shown, older := newestItems(items, toolListLimit)
	if len(shown) != toolListLimit || older != 70 || shown[0] != 70 || shown[len(shown)-1] != 119 {
		t.Fatalf("newestItems = %d items from %d, %d older", len(shown), shown[0], older)
	}
}
