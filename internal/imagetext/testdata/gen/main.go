// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build ignore

// Command gen renders the imagetext test fixtures (🎯T403.7.1).
//
// The fixtures are committed PNGs rather than generated at test time, because
// a test that renders its own input tests the renderer as much as the
// recogniser: a font substitution or a rasteriser change would move the
// ground truth along with the answer. Run this by hand when the fixtures need
// to change, and re-check the ground-truth strings in the test:
//
//	go run ./internal/imagetext/testdata/gen -out internal/imagetext/testdata
//
// It needs a system TrueType font (macOS Arial by default); only the rendered
// PNGs are committed.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func main() {
	out := flag.String("out", ".", "directory to write fixtures into")
	fontPath := flag.String("font", "/System/Library/Fonts/Supplemental/Arial.ttf", "TrueType font")
	flag.Parse()

	data, err := os.ReadFile(*fontPath)
	if err != nil {
		log.Fatalf("read font: %v", err)
	}
	f, err := opentype.Parse(data)
	if err != nil {
		log.Fatalf("parse font: %v", err)
	}

	if err := writeSimple(f, filepath.Join(*out, "simple.png")); err != nil {
		log.Fatalf("simple: %v", err)
	}
	if err := writeDense(f, filepath.Join(*out, "dense.png")); err != nil {
		log.Fatalf("dense: %v", err)
	}
}

// writeSimple renders a small dialog-like image whose strings any pass reads.
func writeSimple(f *opentype.Font, path string) error {
	const w, h = 1200, 400
	img := newCanvas(w, h, color.Gray{Y: 0xf7})
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 34, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return err
	}
	defer face.Close()
	lines := []struct {
		x, y int
		s    string
	}{
		{60, 90, "Deployment failed"},
		{60, 160, "checkout-service v4.12.0"},
		{60, 230, "Error: connection refused on port 8443"},
		{60, 300, "Retry in 30 seconds"},
	}
	for _, l := range lines {
		drawString(img, face, l.x, l.y, l.s, color.Gray{Y: 0x11})
	}
	return writePNG(path, img)
}

// writeDense renders a Retina-scale grid of small text: a 1700x900 point
// window at 2x, with ~6pt body text, so glyphs are ~12 physical pixels.
//
// That is small enough that the whole-image pass loses it to downsampling and
// the 2x2 tile pass, which submits each quadrant at its own scale, does not.
func writeDense(f *opentype.Font, path string) error {
	const w, h = 3400, 1800
	img := newCanvas(w, h, color.Gray{Y: 0xff})
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 12, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return err
	}
	defer face.Close()

	// A clinical-admin style grid: header row, then rows of short cells. The
	// ground-truth strings the test looks for are the distinctive ones, spread
	// across all four quadrants so no single tile carries the whole answer.
	headers := []string{"Patient", "MRN", "Ward", "Admitted", "Consultant", "Status"}
	rows := [][]string{
		{"Abernathy, R", "MRN-40218", "Ward 3B", "2026-08-02", "Dr Okonkwo", "Discharged"},
		{"Baptiste, L", "MRN-40219", "Ward 1A", "2026-08-03", "Dr Halvorsen", "Awaiting review"},
		{"Castellanos, M", "MRN-40220", "Ward 3B", "2026-08-03", "Dr Okonkwo", "In theatre"},
		{"Dziedzic, K", "MRN-40221", "Ward 2C", "2026-08-04", "Dr Prasanna", "Stable"},
		{"Ekwueme, N", "MRN-40222", "Ward 1A", "2026-08-04", "Dr Halvorsen", "Observation"},
	}

	const (
		colW    = 280
		rowH    = 26
		originX = 40
		originY = 40
	)
	// Repeat the block across the canvas so every quadrant carries dense text.
	for blockRow := 0; blockRow*(rowH*(len(rows)+2)) < h-60; blockRow++ {
		for blockCol := 0; blockCol*(colW*len(headers)+60) < w-60; blockCol++ {
			bx := originX + blockCol*(colW*len(headers)+60)
			by := originY + blockRow*(rowH*(len(rows)+2))
			for i, hdr := range headers {
				drawString(img, face, bx+i*colW, by, hdr, color.Gray{Y: 0x33})
			}
			for r, row := range rows {
				for c, cell := range row {
					drawString(img, face, bx+c*colW, by+(r+1)*rowH, cell, color.Gray{Y: 0x11})
				}
			}
		}
	}
	return writePNG(path, img)
}

func newCanvas(w, h int, bg color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)
	return img
}

func drawString(dst *image.RGBA, face font.Face, x, y int, s string, c color.Color) {
	d := &font.Drawer{
		Dst:  dst,
		Src:  &image.Uniform{C: c},
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return err
	}
	st, err := f.Stat()
	if err == nil {
		fmt.Printf("wrote %s (%d bytes)\n", path, st.Size())
	}
	return nil
}
