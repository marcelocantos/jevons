// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"image"
	"testing"
)

// 🎯T976: a thumb is sized to how the transcript draws it (120 CSS px tall,
// up to the bubble width, at 2x). The owner's 2000x468 screenshot was cut to
// 320x75 by the old longest-edge cap and drawn ~443 CSS px wide: about 3x
// upscaled on a Retina display, visibly smeared.
func TestT976ThumbFitsTheDrawnBoxAt2x(t *testing.T) {
	for _, c := range []struct {
		name         string
		w, h         int
		wantW, wantH int
	}{
		{"wide screenshot", 2000, 468, 960, 225},
		{"tall capture", 468, 2000, 56, 240},
		{"square photo", 1200, 1200, 240, 240},
		{"already small", 300, 100, 300, 100},
	} {
		got := resizeFit(image.NewRGBA(image.Rect(0, 0, c.w, c.h)), ImageThumbMaxWidth, ImageThumbMaxHeight).Bounds()
		if got.Dx() != c.wantW || got.Dy() != c.wantH {
			t.Errorf("%s %dx%d -> %dx%d, want %dx%d", c.name, c.w, c.h, got.Dx(), got.Dy(), c.wantW, c.wantH)
		}
	}
	// The drawn width of a wide screenshot in a ~443 CSS px bubble at 2x is
	// ~886 device px; the thumb must cover it.
	if w := resizeFit(image.NewRGBA(image.Rect(0, 0, 2000, 468)), ImageThumbMaxWidth, ImageThumbMaxHeight).Bounds().Dx(); w < 886 {
		t.Fatalf("wide thumb %d px would be upscaled into an 886 px draw", w)
	}
}
