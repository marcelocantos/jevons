// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin && cgo

package imagetext

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework Vision -framework ImageIO -framework CoreGraphics
#include <stdlib.h>
char *jv_vision_recognize(const char *cpath, double rx, double ry, double rw, double rh, char **errOut);
*/
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
	"unsafe"
)

// Available reports whether a deterministic recogniser is compiled in.
func Available() bool { return true }

// UnavailableReason is empty where Available is true.
func UnavailableReason() string { return "" }

// visionLine is the wire shape of one observation from the Objective-C side,
// in Vision's own bottom-left normalised coordinates.
type visionLine struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	W          float64 `json:"w"`
	H          float64 `json:"h"`
}

// recognizeRegion runs Vision over one normalised top-left-origin region of
// the image and returns lines in the whole image's top-left-origin space.
func recognizeRegion(path string, region Rect, pass string) ([]Line, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	var cerr *C.char
	res := C.jv_vision_recognize(cpath, C.double(region.X), C.double(region.Y),
		C.double(region.W), C.double(region.H), &cerr)
	if res == nil {
		msg := "vision failed"
		if cerr != nil {
			msg = C.GoString(cerr)
			C.free(unsafe.Pointer(cerr))
		}
		return nil, fmt.Errorf("%s", msg)
	}
	raw := C.GoString(res)
	C.free(unsafe.Pointer(res))

	var parsed struct {
		Lines []visionLine `json:"lines"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("vision result: %w", err)
	}

	out := make([]Line, 0, len(parsed.Lines))
	for _, l := range parsed.Lines {
		// Vision's origin is bottom-left; flip to top-left within the region,
		// then map the region into the whole image.
		local := Rect{X: l.X, Y: 1 - (l.Y + l.H), W: l.W, H: l.H}
		out = append(out, Line{
			Text:       l.Text,
			Confidence: l.Confidence,
			Rect:       MapToWhole(region, local),
			Pass:       pass,
		})
	}
	return out, nil
}

// Extract runs the whole-image pass and the 2x2 tile pass over path and
// returns their merged result.
//
// Both passes always run. The tile pass is not conditional on the whole pass
// coming back thin, because "thin" is exactly what a page of small text and a
// page with three words look like from the outside — and a conditional second
// pass is a second pass that stops running the day the condition drifts.
//
// A failed whole pass is fatal (nothing was recognised, and the caller must be
// told so rather than shown a partial). A failed individual tile is not: the
// other passes still saw what they saw, and dropping a tile loses coverage,
// never adds invention.
func Extract(ctx context.Context, path, imageID string) Extraction {
	started := time.Now()
	e := Extraction{
		Version:     SchemaVersion,
		ImageID:     imageID,
		Source:      SourceVision,
		ExtractedAt: started.UTC(),
	}
	finish := func() Extraction {
		e.DurationMS = time.Since(started).Milliseconds()
		return e
	}
	degrade := func(reason string) Extraction {
		e.Source = SourceUnavailable
		e.Degraded = true
		e.Reason = reason
		e.Lines = nil
		return finish()
	}

	if _, err := os.Stat(path); err != nil {
		return degrade(fmt.Sprintf("image unreadable: %v", err))
	}
	if err := ctx.Err(); err != nil {
		return degrade(fmt.Sprintf("cancelled before recognition: %v", err))
	}

	whole, err := recognizeRegion(path, Rect{X: 0, Y: 0, W: 1, H: 1}, PassWhole)
	if err != nil {
		return degrade(fmt.Sprintf("whole-image pass failed: %v", err))
	}

	var tiled []Line
	tiles := 0
	for _, t := range Tiles2x2() {
		if ctx.Err() != nil {
			// Out of time: keep what the passes so far recognised. Every line
			// held is one a recogniser actually read.
			e.Reason = fmt.Sprintf("tile pass cut short after %d of 4 tiles: %v", tiles, ctx.Err())
			break
		}
		lines, err := recognizeRegion(path, t.Rect, PassTile)
		if err != nil {
			e.Reason = fmt.Sprintf("tile %d,%d failed: %v", t.Row, t.Col, err)
			continue
		}
		tiles++
		tiled = append(tiled, lines...)
	}

	e.Lines = Merge(whole, tiled)
	e.Tiles = tiles
	return finish()
}
