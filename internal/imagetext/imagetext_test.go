// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package imagetext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The whole point of this package is that nothing enters durable context
// except what a recogniser actually read. Every test below is a way for an
// invented statement to get in, closed.

func TestTiles2x2CoversTheImageAndOverlapsTheSeams(t *testing.T) {
	tiles := Tiles2x2()
	if len(tiles) != 4 {
		t.Fatalf("want 4 tiles, got %d", len(tiles))
	}
	for _, tl := range tiles {
		r := tl.Rect
		if r.X < 0 || r.Y < 0 || r.Right() > 1.0000001 || r.Bottom() > 1.0000001 {
			t.Errorf("tile %d,%d escapes the image: %+v", tl.Row, tl.Col, r)
		}
		// Each tile must reach past its own half, or a line of text sitting on
		// the midline is cut in two and half a line reads as a whole
		// observation that was never made.
		if r.W <= 0.5 || r.H <= 0.5 {
			t.Errorf("tile %d,%d does not overlap the seam: %+v", tl.Row, tl.Col, r)
		}
	}
	// Union covers every corner and the centre.
	for _, p := range [][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}, {0.5, 0.5}} {
		covered := false
		for _, tl := range tiles {
			r := tl.Rect
			if p[0] >= r.X && p[0] <= r.Right() && p[1] >= r.Y && p[1] <= r.Bottom() {
				covered = true
			}
		}
		if !covered {
			t.Errorf("point %v covered by no tile", p)
		}
	}
}

func TestMapToWholePlacesATileLineInTheImage(t *testing.T) {
	tile := Rect{X: 0.5, Y: 0.5, W: 0.5, H: 0.5}
	// Dead centre of the bottom-right tile is 3/4, 3/4 of the whole image.
	got := MapToWhole(tile, Rect{X: 0.5, Y: 0.5, W: 0.2, H: 0.1})
	want := Rect{X: 0.75, Y: 0.75, W: 0.1, H: 0.05}
	if !nearRect(got, want) {
		t.Errorf("MapToWhole = %+v, want %+v", got, want)
	}
}

func TestMergeDropsLowConfidenceNoise(t *testing.T) {
	whole := []Line{
		{Text: "Deployment failed", Confidence: 1, Rect: Rect{0, 0, 0.5, 0.05}, Pass: PassWhole},
		{Text: "nil hủn il col vinh vilni volt", Confidence: 0.3, Rect: Rect{0, 0.2, 0.5, 0.05}, Pass: PassWhole},
	}
	got := Merge(whole, nil)
	if len(got) != 1 || got[0].Text != "Deployment failed" {
		t.Fatalf("low-confidence nonsense survived the floor: %+v", got)
	}
}

func TestMergeKeepsTheFullerReadingOfOneLine(t *testing.T) {
	// The whole pass saw a fragment at the tile seam; a tile read the line.
	// Both describe the same place, so the fragment must not be shown as its
	// own observation.
	whole := []Line{{Text: "ort 8443", Confidence: 1, Rect: Rect{0.45, 0.5, 0.1, 0.03}, Pass: PassWhole}}
	tiles := []Line{{Text: "Error: connection refused on port 8443", Confidence: 1,
		Rect: Rect{0.05, 0.5, 0.6, 0.03}, Pass: PassTile}}
	got := Merge(whole, tiles)
	if len(got) != 1 {
		t.Fatalf("want one line, got %d: %+v", len(got), got)
	}
	if got[0].Text != "Error: connection refused on port 8443" {
		t.Errorf("kept the fragment over the full line: %q", got[0].Text)
	}
}

func TestMergeKeepsRepeatedTextInDifferentPlaces(t *testing.T) {
	// A screenshot may legitimately say "OK" three times. Collapsing them is
	// a silent edit of what the image says.
	var tiles []Line
	for _, y := range []float64{0.1, 0.4, 0.7} {
		tiles = append(tiles, Line{Text: "OK", Confidence: 1,
			Rect: Rect{0.1, y, 0.05, 0.03}, Pass: PassTile})
	}
	got := Merge(nil, tiles)
	if len(got) != 3 {
		t.Fatalf("want 3 separate OKs, got %d: %+v", len(got), got)
	}
}

func TestMergeDeduplicatesTheSameLineSeenByTwoTiles(t *testing.T) {
	same := Rect{0.4, 0.5, 0.2, 0.03}
	tiles := []Line{
		{Text: "Awaiting review", Confidence: 1, Rect: same, Pass: PassTile},
		{Text: "Awaiting review", Confidence: 1, Rect: same, Pass: PassTile},
	}
	if got := Merge(nil, tiles); len(got) != 1 {
		t.Fatalf("overlapping tiles duplicated a line: %+v", got)
	}
}

func TestReadingOrderIsTopToBottomThenLeftToRight(t *testing.T) {
	in := []Line{
		{Text: "c", Rect: Rect{0.1, 0.60, 0.1, 0.04}, Confidence: 1},
		{Text: "b", Rect: Rect{0.5, 0.10, 0.1, 0.04}, Confidence: 1},
		{Text: "a", Rect: Rect{0.1, 0.11, 0.1, 0.04}, Confidence: 1},
	}
	got := ReadingOrder(in)
	if want := "a b c"; strings.Join(texts(got), " ") != want {
		t.Errorf("reading order = %v, want %v", texts(got), want)
	}
}

func TestBlockNamesItsOwnLimits(t *testing.T) {
	e := Extraction{ImageID: "abc", Source: SourceVision, Lines: []Line{
		{Text: "Deployment failed", Confidence: 1, Rect: Rect{0, 0, 0.5, 0.05}},
	}}
	b := Block(e)
	if !strings.Contains(b, "Deployment failed") {
		t.Errorf("block omits the recognised text: %q", b)
	}
	if !strings.Contains(b, "incomplete") || !strings.Contains(b, "look at the image") {
		t.Errorf("block does not tell the reader what it is blind to: %q", b)
	}
	if !strings.Contains(b, SourceVision) {
		t.Errorf("block does not say what read the image: %q", b)
	}
}

func TestBlockOnDegradedSaysNothingAboutTheImage(t *testing.T) {
	e := Extraction{ImageID: "abc", Source: SourceUnavailable, Degraded: true,
		Reason: "no OS text recogniser on linux/amd64"}
	b := Block(e)
	if !strings.Contains(b, "no OS text recogniser on linux/amd64") {
		t.Errorf("degraded block hides the reason: %q", b)
	}
	if !strings.Contains(b, "do not describe it from memory or inference") {
		t.Errorf("degraded block invites a fabricated description: %q", b)
	}
}

// A recogniser that ran and found nothing is a real answer about the image.
// A recogniser that never ran is not. Collapsing the two would let "no text"
// be asserted about an image nobody read.
func TestBlockDistinguishesNoTextFromNoRecogniser(t *testing.T) {
	ran := Block(Extraction{ImageID: "a", Source: SourceVision})
	never := Block(Extraction{ImageID: "a", Source: SourceUnavailable, Degraded: true, Reason: "cgo disabled"})
	if !strings.Contains(ran, "found no text") {
		t.Errorf("ran-and-found-nothing does not say so: %q", ran)
	}
	if strings.Contains(never, "found no text") {
		t.Errorf("degraded block claims the image has no text: %q", never)
	}
}

func TestModelDescriptionIsTaggedAsNotObservation(t *testing.T) {
	got := TagModelDescription("abc", "gemma3:12b", "A dashboard showing three red alerts")
	if !strings.Contains(got, "DESCRIPTION WRITTEN BY gemma3:12b") ||
		!strings.Contains(got, "not observation") {
		t.Errorf("model description is not marked as model-authored: %q", got)
	}
	if !strings.Contains(got, "A dashboard showing three red alerts") {
		t.Errorf("tagging dropped the description: %q", got)
	}
}

func TestMarkersFindsImageIDsInOrderWithoutRepeats(t *testing.T) {
	text := "before [image: A1B2] middle [image: cafe01] and [image: a1b2] again"
	got := Markers(text)
	if len(got) != 2 || got[0] != "a1b2" || got[1] != "cafe01" {
		t.Fatalf("Markers = %v", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Extraction{
		Version: SchemaVersion, ImageID: "cafe01", Source: SourceVision,
		Lines:       []Line{{Text: "Ward 3B", Confidence: 1, Rect: Rect{0.1, 0.2, 0.1, 0.02}, Pass: PassTile}},
		Tiles:       4,
		ExtractedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := Save(dir, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(dir, "cafe01")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Lines) != 1 || got.Lines[0].Text != "Ward 3B" || got.Lines[0].Pass != PassTile {
		t.Errorf("round trip lost lines: %+v", got)
	}
	if got.Tiles != 4 || got.Source != SourceVision {
		t.Errorf("round trip lost provenance: %+v", got)
	}
}

// A sidecar that will not parse must be an error, never an empty Extraction:
// an empty Extraction is the claim "the recogniser found no text here", and
// inventing that claim out of a parse failure is the fabrication this package
// exists to stop.
func TestLoadRefusesToInventAnEmptyExtraction(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cafe01"+SidecarSuffix), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, "cafe01"); err == nil {
		t.Fatal("malformed sidecar loaded as a valid extraction")
	}
	if _, ok := DirLoader(dir)("cafe01"); ok {
		t.Fatal("DirLoader reported a malformed sidecar as known")
	}
}

func TestExpandAppendsTheExtractionAndKeepsTheMarker(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Extraction{Version: SchemaVersion, ImageID: "cafe01", Source: SourceVision,
		Lines: []Line{{Text: "Deployment failed", Confidence: 1, Rect: Rect{0, 0, 0.4, 0.05}, Pass: PassWhole}}}); err != nil {
		t.Fatal(err)
	}
	in := "what went wrong here? [image: cafe01]"
	got := Expand(in, DirLoader(dir))
	if !strings.Contains(got, "[image: cafe01]") {
		t.Errorf("Expand ate the marker the client renders: %q", got)
	}
	if !strings.Contains(got, "Deployment failed") {
		t.Errorf("Expand did not carry the extracted text: %q", got)
	}
}

func TestExpandSaysNothingAboutAnImageItKnowsNothingAbout(t *testing.T) {
	dir := t.TempDir()
	in := "look at this [image: deadbeef]"
	if got := Expand(in, DirLoader(dir)); got != in {
		t.Errorf("Expand manufactured a statement about an unknown image: %q", got)
	}
	if got := Expand("no images here", DirLoader(dir)); got != "no images here" {
		t.Errorf("Expand altered text with no markers: %q", got)
	}
	if got := Expand(in, nil); got != in {
		t.Errorf("Expand with no loader changed the text: %q", got)
	}
}

func texts(lines []Line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Text
	}
	return out
}

func nearRect(a, b Rect) bool {
	const eps = 1e-9
	near := func(x, y float64) bool { return x-y < eps && y-x < eps }
	return near(a.X, b.X) && near(a.Y, b.Y) && near(a.W, b.W) && near(a.H, b.H)
}
