// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"image"
	"testing"
)

// 🎯T976: a thumb is sized to the compact box the transcript draws it in
// (at most 320x120 CSS px, aspect kept) at 2x, so it is sharp and never
// enlarged. The old rule stretched a wide screenshot across the bubble.
func TestT976ThumbFitsTheDrawnBoxAt2x(t *testing.T) {
	for _, c := range []struct {
		name         string
		w, h         int
		wantW, wantH int
	}{
		{"wide screenshot", 2000, 468, 640, 150},
		{"tall capture", 468, 2000, 56, 240},
		{"square photo", 1200, 1200, 240, 240},
		{"already small", 300, 100, 300, 100},
	} {
		got := resizeFit(image.NewRGBA(image.Rect(0, 0, c.w, c.h)), ImageThumbMaxWidth, ImageThumbMaxHeight).Bounds()
		if got.Dx() != c.wantW || got.Dy() != c.wantH {
			t.Errorf("%s %dx%d -> %dx%d, want %dx%d", c.name, c.w, c.h, got.Dx(), got.Dy(), c.wantW, c.wantH)
		}
	}
	// The widest draw is 320 CSS px, 640 device px at 2x; the thumb covers it.
	if w := resizeFit(image.NewRGBA(image.Rect(0, 0, 2000, 468)), ImageThumbMaxWidth, ImageThumbMaxHeight).Bounds().Dx(); w < 640 {
		t.Fatalf("wide thumb %d px would be upscaled into a 640 px draw", w)
	}
}
