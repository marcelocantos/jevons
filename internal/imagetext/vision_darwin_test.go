// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin && cgo

package imagetext

import (
	"context"
	"strings"
	"testing"
	"time"
)

// simpleGroundTruth is what testdata/simple.png was rendered from
// (testdata/gen/main.go). The fixture is a committed PNG rather than one the
// test renders, so a font substitution or a rasteriser change cannot move the
// ground truth along with the answer.
var simpleGroundTruth = []string{
	"Deployment failed",
	"checkout-service v4.12.0",
	"Error: connection refused on port 8443",
	"Retry in 30 seconds",
}

// A fixture with known strings must reach the PERSISTED extraction — the
// sidecar is what every later reader of the thread sees instead of the pixels,
// so that is where the assertion belongs, not on the in-memory result.
func TestSimpleFixtureStringsReachTheSidecar(t *testing.T) {
	dir := t.TempDir()
	e := Extract(t.Context(), "testdata/simple.png", "simple01")
	if e.Degraded {
		t.Fatalf("recognition degraded: %s", e.Reason)
	}
	if err := Save(dir, e); err != nil {
		t.Fatalf("Save: %v", err)
	}
	stored, err := Load(dir, "simple01")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := stored.Text()
	for _, want := range simpleGroundTruth {
		if !strings.Contains(got, want) {
			t.Errorf("persisted extraction is missing %q\ngot:\n%s", want, got)
		}
	}
	if stored.Source != SourceVision {
		t.Errorf("source = %q, want %q", stored.Source, SourceVision)
	}
	// The block a model actually receives carries the text and its limits.
	block := Block(stored)
	if !strings.Contains(block, simpleGroundTruth[0]) || !strings.Contains(block, "incomplete") {
		t.Errorf("block is not what a model needs:\n%s", block)
	}
}

// The tile pass exists because the whole-image pass loses small text to
// downsampling. This is the test that tells a wired tile pass from dead code:
// a ground-truth string the whole pass cannot read, recovered by tiles.
//
// On testdata/dense.png (3400x1800, ~6pt grid) the whole pass returns nothing
// above the confidence floor at all — its eight observations are all 0.30
// nonsense of the "nil hủn il col vinh" kind — while the tile pass reads the
// ward column verbatim.
func TestDenseFixtureNeedsTheTilePass(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	wholeOnly, err := recognizeRegion("testdata/dense.png", Rect{X: 0, Y: 0, W: 1, H: 1}, PassWhole)
	if err != nil {
		t.Fatalf("whole pass: %v", err)
	}
	wholeText := strings.Join(texts(Merge(wholeOnly, nil)), "\n")

	e := Extract(ctx, "testdata/dense.png", "dense01")
	if e.Degraded {
		t.Fatalf("recognition degraded: %s", e.Reason)
	}
	if e.Tiles != 4 {
		t.Errorf("tiles recognised = %d, want 4", e.Tiles)
	}
	merged := e.Text()

	// Ground truth from testdata/gen/main.go, chosen because it is spread
	// across quadrants: a single tile cannot carry the whole answer.
	const denseGroundTruth = "Ward 2C"
	if strings.Contains(wholeText, denseGroundTruth) {
		t.Fatalf("whole pass already reads %q — the fixture no longer proves the tile pass:\n%s",
			denseGroundTruth, wholeText)
	}
	if !strings.Contains(merged, denseGroundTruth) {
		t.Fatalf("tile pass did not recover %q\nmerged:\n%s", denseGroundTruth, merged)
	}
	if e.TileLines() == 0 {
		t.Error("no line is attributed to the tile pass")
	}
}

// A recogniser that cannot run degrades to image-only with the reason
// recorded. It must never fall back to a description, and it must not be
// mistakable for "this image has no text".
func TestUnreadableImageDegradesWithAReason(t *testing.T) {
	e := Extract(t.Context(), "testdata/does-not-exist.png", "missing01")
	if !e.Degraded {
		t.Fatal("missing image was not reported as degraded")
	}
	if e.Reason == "" {
		t.Error("degraded extraction records no reason")
	}
	if len(e.Lines) != 0 {
		t.Errorf("degraded extraction carries lines: %+v", e.Lines)
	}
	if e.Source != SourceUnavailable {
		t.Errorf("source = %q, want %q", e.Source, SourceUnavailable)
	}
}

// Recognition is bounded by the caller's context: a cancelled extraction
// reports what it has, and says why it stopped, rather than blocking a paste.
func TestCancelledContextDegradesRatherThanHanging(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e := Extract(ctx, "testdata/simple.png", "cancelled01")
	if !e.Degraded || e.Reason == "" {
		t.Fatalf("cancelled extraction did not degrade with a reason: %+v", e)
	}
}
