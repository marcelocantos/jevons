// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package imagetext

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MarkerPrefix opens the journal/chat marker for a stored image (🎯T76/T224).
// internal/server.ImageMarkerPrefix is defined from this constant so the
// producer and the consumer of the marker cannot drift apart.
const MarkerPrefix = "[image: "

// SidecarSuffix is appended to an image id to name its extraction sidecar.
// It sits next to the image itself: the extraction is a property of that
// attachment, and a reader holding the image can always find what was read
// out of it without consulting an index.
const SidecarSuffix = ".text.json"

// markerRe matches [image: <hex id>] as written by server.ImageMarker.
var markerRe = regexp.MustCompile(`\[image:\s*([0-9a-fA-F]+)\s*\]`)

// Markers returns the distinct image ids referenced by text, in order of first
// appearance, lowercased to match on-disk names.
func Markers(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range markerRe.FindAllStringSubmatch(text, -1) {
		id := strings.ToLower(m[1])
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// SidecarPath is where the extraction for id lives, given the images dir.
func SidecarPath(imagesDir, id string) string {
	return filepath.Join(imagesDir, id+SidecarSuffix)
}

// Save writes the extraction sidecar atomically (write-and-rename).
//
// A half-written sidecar would be a durable record of an extraction that never
// happened, so the file is only ever observed complete or absent.
func Save(imagesDir string, e Extraction) error {
	if e.ImageID == "" {
		return fmt.Errorf("imagetext: extraction has no image id")
	}
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := SidecarPath(imagesDir, e.ImageID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Load reads the extraction sidecar for id.
//
// A malformed sidecar is an error, never a silently empty extraction: an empty
// Extraction claims "the recogniser found no text", and inventing that claim
// out of a parse failure is the same class of fabrication this package exists
// to stop.
func Load(imagesDir, id string) (Extraction, error) {
	var e Extraction
	data, err := os.ReadFile(SidecarPath(imagesDir, id))
	if err != nil {
		return Extraction{}, err
	}
	if err := json.Unmarshal(data, &e); err != nil {
		return Extraction{}, fmt.Errorf("imagetext: sidecar %s: %w", id, err)
	}
	return e, nil
}

// Loader answers with the stored extraction for an image id. The bool is false
// when nothing is known about that id — no sidecar, or one that would not
// parse. Expand renders nothing at all in that case rather than asserting
// absence of text.
type Loader func(id string) (Extraction, bool)

// DirLoader is a Loader over an images directory.
func DirLoader(imagesDir string) Loader {
	return func(id string) (Extraction, bool) {
		e, err := Load(imagesDir, id)
		if err != nil {
			return Extraction{}, false
		}
		return e, true
	}
}

// Expand appends the extraction block for every image marker in text.
//
// The marker itself is left exactly where it is — clients render it as a
// thumbnail — and the extracted text is appended below the message, so a model
// receiving the turn gets the image and its text together. Text with no
// markers, or markers with no extraction on disk, comes back unchanged: this
// function never manufactures a statement about an image.
func Expand(text string, load Loader) string {
	ids := Markers(text)
	if len(ids) == 0 || load == nil {
		return text
	}
	var blocks []string
	for _, id := range ids {
		e, ok := load(id)
		if !ok {
			continue
		}
		blocks = append(blocks, Block(e))
	}
	if len(blocks) == 0 {
		return text
	}
	return strings.TrimRight(text, "\n") + "\n\n" + strings.Join(blocks, "\n\n")
}
