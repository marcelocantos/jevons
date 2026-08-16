// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package imagetext extracts text from an image with the host OS text
// recogniser and renders it for durable context (🎯T403.7.1).
//
// The point is provenance, not description quality. A model-written
// description of a pasted image is written once, at paste time, and from then
// on it IS the transcript's memory of the image: every fleet agent that reads
// the thread without the pixels inherits it as fact. Measured on 2026-08-10
// over 10 screenshots and 90 independent judgements, eager rich description
// scored 5.0/10 at best (truthfulness 2.4/5) with 7-9 fabricated claims per
// description, under a prompt that explicitly forbade guessing — so what gets
// laundered into durable context is mostly invention. The same models
// ANSWERING a question about an image scored 72.5%, and 90% when OCR text
// accompanied the image.
//
// So this package writes what a deterministic recogniser saw and nothing else.
// Vision cannot hallucinate: it either returns a string it recognised or it
// returns nothing. What it is blind to (stylised and in-scene text, colour,
// shape, count, layout) is named in the block it renders, so a model reading
// the extraction knows the extraction is partial and asks the pixels for the
// rest.
package imagetext

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// SchemaVersion is the on-disk sidecar version. Bump on incompatible change.
const SchemaVersion = 1

// Source values name the extractor that produced an Extraction. The source is
// persisted so a reader can tell a deterministic recognition from anything
// else that might later write into the same slot.
const (
	// SourceVision is macOS Vision VNRecognizeTextRequest at accurate level.
	SourceVision = "macos-vision-accurate"
	// SourceUnavailable means no recogniser ran; Reason says why.
	SourceUnavailable = "unavailable"
)

// Pass labels which recognition pass produced a line. Kept on the line
// because the tile pass exists precisely to catch what the whole-image pass
// misses: without the label, a dead tile pass is indistinguishable from a
// redundant one.
const (
	PassWhole = "whole"
	PassTile  = "tile"
)

// Rect is a normalised box over the whole image with a TOP-LEFT origin:
// X and Y grow right and down, all values in 0..1.
//
// Vision's own boundingBox is bottom-left origin; the conversion happens once,
// at the recogniser boundary, so every consumer here reads in the same
// direction the text does.
type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// CenterY is the vertical midpoint, used for reading-order row grouping.
func (r Rect) CenterY() float64 { return r.Y + r.H/2 }

// Bottom is the lower edge (larger Y).
func (r Rect) Bottom() float64 { return r.Y + r.H }

// Right is the right edge.
func (r Rect) Right() float64 { return r.X + r.W }

// Overlaps reports whether two boxes intersect at all.
func (r Rect) Overlaps(o Rect) bool {
	return r.X < o.Right() && o.X < r.Right() && r.Y < o.Bottom() && o.Y < r.Bottom()
}

// Line is one recognised string with where it was found.
type Line struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	Rect       Rect    `json:"rect"`
	// Pass is PassWhole or PassTile — which pass first produced this line.
	Pass string `json:"pass"`
}

// Extraction is the durable record of one image's recognised text.
//
// A zero-line Extraction is a real answer ("the recogniser ran and found no
// text"), distinct from a degraded one ("no recogniser ran"). Consumers must
// not collapse the two: the first says the image has no text, the second says
// nothing at all about the image.
type Extraction struct {
	Version     int       `json:"version"`
	ImageID     string    `json:"image_id"`
	Source      string    `json:"source"`
	Lines       []Line    `json:"lines"`
	Degraded    bool      `json:"degraded"`
	Reason      string    `json:"reason,omitempty"`
	DurationMS  int64     `json:"duration_ms"`
	ExtractedAt time.Time `json:"extracted_at"`
	// Tiles is how many tile-pass regions were recognised (0 = whole only).
	Tiles int `json:"tiles"`
}

// TileOverlap is the fraction of each axis by which the 2x2 tiles overrun the
// half they cover. Non-overlapping quarters cut every string that straddles
// the midline in two, and half a string is worse than no string — it reads as
// a real observation. 0.08 is enough for a line of body text at the seam.
const TileOverlap = 0.08

// Tile is a normalised sub-region of the image submitted as its own
// recognition pass, with a TOP-LEFT origin like Rect.
type Tile struct {
	Row, Col int
	Rect     Rect
}

// Tiles2x2 returns the four overlapping quadrants of the tile pass.
//
// Why a second pass at all: the recogniser works from a bounded internal
// raster, so a 3400x1800 screenshot of ~6pt grid text arrives downsampled past
// legibility and the whole-image pass returns nothing for it. Each quadrant is
// half the linear size, so the same text arrives at roughly twice the scale.
// Measured on a dense clinical-admin grid where every local model from 2B to
// 31B scored 1-2 of 8, the tiled pass recovered all 10 ground-truth strings in
// 2.07s.
func Tiles2x2() []Tile {
	const half = 0.5
	tiles := make([]Tile, 0, 4)
	for row := range 2 {
		for col := range 2 {
			x := float64(col) * half
			y := float64(row) * half
			w, h := half, half
			// Grow each tile toward the seam it borders, clamped to the image.
			if col == 0 {
				w += TileOverlap
			} else {
				x -= TileOverlap
				w += TileOverlap
			}
			if row == 0 {
				h += TileOverlap
			} else {
				y -= TileOverlap
				h += TileOverlap
			}
			tiles = append(tiles, Tile{Row: row, Col: col, Rect: clampRect(Rect{X: x, Y: y, W: w, H: h})})
		}
	}
	return tiles
}

func clampRect(r Rect) Rect {
	if r.X < 0 {
		r.W += r.X
		r.X = 0
	}
	if r.Y < 0 {
		r.H += r.Y
		r.Y = 0
	}
	if r.Right() > 1 {
		r.W = 1 - r.X
	}
	if r.Bottom() > 1 {
		r.H = 1 - r.Y
	}
	return r
}

// MapToWhole converts a rect expressed in a tile's own 0..1 space into the
// whole image's 0..1 space.
func MapToWhole(tile Rect, r Rect) Rect {
	return Rect{
		X: tile.X + r.X*tile.W,
		Y: tile.Y + r.Y*tile.H,
		W: r.W * tile.W,
		H: r.H * tile.H,
	}
}

// MinConfidence is the floor below which a recognised string is discarded.
//
// Deterministic does not mean never wrong. Asked to read a 3400x1800 grid of
// ~6pt text it cannot resolve, Vision does not return nothing — it returns
// plausible-looking nonsense ("nil hủn il col vinh vilni volt chinh viil") at
// confidence 0.30, while every string it actually read comes back at 1.00.
// Nonsense at 0.30 admitted into durable context is the exact failure this
// package exists to prevent, merely arriving from a different direction.
//
// Where to put the floor was measured rather than guessed, over 41 of the
// owner's own screenshots in ~/.jevons/images (1736 observations across the
// whole and tile passes). Vision reports essentially three confidences: 1610
// lines at 1.00, 108 at 0.50, 18 at 0.30. The 0.30 band is mostly garbage
// ("bane can c", "httpp.//vaom/ilatotiic"). The 0.50 band is NOT: it is real
// content Vision was merely unsure of — "• jevons-po", "• jv-t114", "target",
// "© T67" (a 🎯 misread). A floor above 0.5 would therefore delete real agent
// names from durable context, which is a silent lie about what the image says.
// So the floor sits between the two bands, and its residual is accepted and
// named: confident garbage does get through — the dense fixture yields one
// 0.50 line of nonsense — because no confidence threshold can separate that
// from the unconfident truth sitting at the same number.
const MinConfidence = 0.4

// Merge folds the tile pass into the whole-image pass and returns one set of
// lines in reading order, dropping low-confidence noise and lines that another
// line already says.
//
// Whole-pass lines are considered first: they were read at the scale the image
// was composed at. A tile line survives only when nothing already in the set
// says the same thing in the same place — which is exactly the line the whole
// pass was too coarse to see.
func Merge(whole, tiles []Line) []Line {
	out := make([]Line, 0, len(whole)+len(tiles))
	for _, l := range append(append([]Line{}, whole...), tiles...) {
		if l.Confidence < MinConfidence || normalizeText(l.Text) == "" {
			continue
		}
		// Tiles overlap each other as well as the whole pass, so a line at a
		// seam can arrive several times; each candidate is judged against
		// everything kept so far, including earlier tiles.
		if i := indexOfOverlapping(out, l); i >= 0 {
			// One box, two readings of it: keep the fuller string. A tile that
			// caught "Error: connection refused on port 8443" and a tile that
			// caught only the fragment "ort 8443" at the seam are the same
			// line, and the fragment shown as its own line reads as a separate
			// observation that was never made.
			if len(normalizeText(l.Text)) > len(normalizeText(out[i].Text)) {
				out[i] = l
			}
			continue
		}
		out = append(out, l)
	}
	return ReadingOrder(out)
}

// indexOfOverlapping returns the index in lines of an entry that is another
// reading of the same line of text as l, or -1.
//
// Two readings are the same line when their boxes overlap AND one string
// contains the other. Position matters on its own: a screenshot may
// legitimately contain "OK" eight times, and collapsing them to one would be a
// silent, invented edit of what the image says. Containment matters on its
// own too: the same words elsewhere in the image are a different observation.
func indexOfOverlapping(lines []Line, l Line) int {
	key := normalizeText(l.Text)
	if key == "" {
		return -1
	}
	for i, c := range lines {
		if !c.Rect.Overlaps(l.Rect) {
			continue
		}
		other := normalizeText(c.Text)
		if strings.Contains(other, key) || strings.Contains(key, other) {
			return i
		}
	}
	return -1
}

// normalizeText is the comparison form for duplicate detection: case-folded
// with runs of whitespace collapsed. It is never persisted or shown — the
// recognised string is kept verbatim.
func normalizeText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// rowOverlapFraction is how much two boxes must share vertically to count as
// the same row of text. Half a line height tolerates ascenders, descenders and
// mixed font sizes on one row without merging adjacent rows.
const rowOverlapFraction = 0.5

// ReadingOrder sorts lines the way the text reads: top to bottom by row, left
// to right within a row. Lines whose vertical spans overlap by at least
// rowOverlapFraction of the shorter box belong to the same row.
//
// The sort is total and deterministic — text then geometry break every
// remaining tie — because this ordering is persisted and diffed.
func ReadingOrder(lines []Line) []Line {
	out := make([]Line, len(lines))
	copy(out, lines)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if sameRow(a.Rect, b.Rect) {
			if a.Rect.X != b.Rect.X {
				return a.Rect.X < b.Rect.X
			}
		} else if a.Rect.CenterY() != b.Rect.CenterY() {
			return a.Rect.CenterY() < b.Rect.CenterY()
		}
		if a.Rect.Y != b.Rect.Y {
			return a.Rect.Y < b.Rect.Y
		}
		return a.Text < b.Text
	})
	return out
}

// sameRow reports whether two boxes are on one row of text.
func sameRow(a, b Rect) bool {
	top := max(a.Y, b.Y)
	bottom := min(a.Bottom(), b.Bottom())
	shared := bottom - top
	if shared <= 0 {
		return false
	}
	shorter := min(a.H, b.H)
	if shorter <= 0 {
		return false
	}
	return shared/shorter >= rowOverlapFraction
}

// Text is the recognised strings, one per line, in reading order.
func (e Extraction) Text() string {
	parts := make([]string, 0, len(e.Lines))
	for _, l := range e.Lines {
		if s := strings.TrimSpace(l.Text); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

// TileLines counts lines that only the tile pass found. Zero on an image with
// no small text is expected; zero across every image means the tile pass is
// dead code.
func (e Extraction) TileLines() int {
	n := 0
	for _, l := range e.Lines {
		if l.Pass == PassTile {
			n++
		}
	}
	return n
}

// blockCaveat is the standing statement of what the extraction is NOT. It ships
// with every block, in the same durable context, because a model that reads
// recognised text without knowing its limits fills the gaps itself — which is
// the failure this whole target exists to prevent.
const blockCaveat = "This extraction is incomplete: it is accurate where it fires, " +
	"blind to stylised and in-scene text, and silent on colour, shape, count and layout. " +
	"For anything it does not say, look at the image."

// Block renders an extraction for durable context: transcript, agent brief or
// event log. It is the whole of what gets written about the image.
func Block(e Extraction) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[image %s — text extracted by %s", e.ImageID, e.Source)
	if e.Degraded {
		reason := e.Reason
		if reason == "" {
			reason = "no reason recorded"
		}
		fmt.Fprintf(&b, "]\nNo text was extracted (%s). The image itself is the only record; "+
			"do not describe it from memory or inference.", reason)
		return b.String()
	}
	if len(e.Lines) == 0 {
		b.WriteString("]\nThe recogniser ran and found no text in this image.\n")
		b.WriteString(blockCaveat)
		return b.String()
	}
	fmt.Fprintf(&b, ", %d line(s)]\n", len(e.Lines))
	b.WriteString(e.Text())
	b.WriteString("\n")
	b.WriteString(blockCaveat)
	return b.String()
}

// ModelDescriptionTag prefixes a model-authored description of an image so it
// can never be read back as observation.
//
// Nothing in the product writes such a description today, and that is the
// point of the target. This exists so that if one ever is written, it is
// written already labelled: the failure mode is not a model describing an
// image, it is a description entering durable context wearing the same clothes
// as a measurement.
func ModelDescriptionTag(imageID, model string) string {
	if model == "" {
		model = "unknown model"
	}
	return fmt.Sprintf("[image %s — DESCRIPTION WRITTEN BY %s, not observation: "+
		"unverified, may contain fabricated detail]", imageID, model)
}

// TagModelDescription returns desc prefixed with its provenance tag.
func TagModelDescription(imageID, model, desc string) string {
	return ModelDescriptionTag(imageID, model) + "\n" + strings.TrimSpace(desc)
}
