// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/chatlog"
)

// validPNG builds a real w×h PNG so thumbnail decode works in hermetics.
func validPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Pix[(y*img.Stride)+x*4+0] = 200
			img.Pix[(y*img.Stride)+x*4+1] = 40
			img.Pix[(y*img.Stride)+x*4+2] = 40
			img.Pix[(y*img.Stride)+x*4+3] = 255
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func postImage(t *testing.T, mux http.Handler, png []byte) map[string]any {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "shot.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(png); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/images", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("upload status %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestImageUploadAndGet(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	pngish := validPNG(t, 32, 24)
	resp := postImage(t, mux, pngish)
	id, _ := resp["id"].(string)
	marker, _ := resp["marker"].(string)
	if id == "" || !strings.Contains(marker, id) {
		t.Fatalf("bad response: %+v", resp)
	}
	// Full file under stateDir/images (thumbs/ is a subdir — not counted as only entry).
	full := filepath.Join(dir, "images", id+".png")
	if st, err := os.Stat(full); err != nil || st.Size() == 0 {
		t.Fatalf("full image missing: %v", err)
	}

	get := httptest.NewRequest(http.MethodGet, "/api/images/"+id, nil)
	gr := httptest.NewRecorder()
	mux.ServeHTTP(gr, get)
	if gr.Code != http.StatusOK {
		t.Fatalf("get status %d", gr.Code)
	}
	got, _ := io.ReadAll(gr.Body)
	if !bytes.Equal(got, pngish) {
		t.Fatalf("round-trip bytes mismatch: len %d vs %d", len(got), len(pngish))
	}
	if ct := gr.Header().Get("Content-Type"); !strings.Contains(ct, "image/png") {
		t.Fatalf("content-type %q", ct)
	}
}

// 🎯T224: upload → thumb_url → journal marker → hydrate/replay → thumb GET.
func TestImageUploadJournalHydrateThumb(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	// Large enough that a thumb would differ if we resized hard; still < max edge so
	// jpeg re-encode is the durable thumb artifact either way.
	pngBytes := validPNG(t, 400, 300)
	resp := postImage(t, mux, pngBytes)
	id, _ := resp["id"].(string)
	marker, _ := resp["marker"].(string)
	thumbURL, _ := resp["thumb_url"].(string)
	if id == "" || marker != ImageMarker(id) {
		t.Fatalf("marker/id: %+v", resp)
	}
	if thumbURL != ImageThumbURL(id) {
		t.Fatalf("thumb_url=%q want %q", thumbURL, ImageThumbURL(id))
	}

	// Thumb file on disk under images/thumbs/
	thumbPath := filepath.Join(dir, "images", "thumbs", id+".jpg")
	if st, err := os.Stat(thumbPath); err != nil || st.Size() == 0 {
		t.Fatalf("thumb missing after upload: %v path=%s", err, thumbPath)
	}

	// Journal records image id marker so hydrate can resolve it.
	logPath := filepath.Join(dir, "chatlog", "jevons.jsonl")
	clog, err := chatlog.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clog.Close() })
	s.SetChatLog(clog)

	echo := chatUserEcho(marker + "\nlook at this")
	if err := clog.Append(echo); err != nil {
		t.Fatal(err)
	}

	// Sealed replay (WS hydrate path) carries the marker.
	var lines []string
	_, _, err = clog.ReplayTailSealed(30, func(line string) error {
		lines = append(lines, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ln := range lines {
		if strings.Contains(ln, marker) && strings.Contains(ln, `"type":"user"`) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("journal hydrate missing marker %q in lines=%v", marker, lines)
	}

	// History API (progressive hydrate) also returns the marker.
	// ReadRange needs the log attached; use handleHistory via mux.
	// end defaults to 0 → window as end=total via query after counting.
	total := 0
	_, total, err = clog.ReplayTail(99, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	hr := httptest.NewRequest(http.MethodGet, "/api/history?end="+itoa(total)+"&limit=50", nil)
	hrr := httptest.NewRecorder()
	mux.ServeHTTP(hrr, hr)
	if hrr.Code != http.StatusOK {
		t.Fatalf("history status %d: %s", hrr.Code, hrr.Body.String())
	}
	if !strings.Contains(hrr.Body.String(), marker) {
		t.Fatalf("history body missing marker: %s", hrr.Body.String())
	}

	// Thumb endpoint serves image/jpeg compact preview.
	tr := httptest.NewRequest(http.MethodGet, ImageThumbURL(id), nil)
	trr := httptest.NewRecorder()
	mux.ServeHTTP(trr, tr)
	if trr.Code != http.StatusOK {
		t.Fatalf("thumb status %d: %s", trr.Code, trr.Body.String())
	}
	if ct := trr.Header().Get("Content-Type"); !strings.Contains(ct, "image/jpeg") {
		t.Fatalf("thumb content-type %q", ct)
	}
	thumbBody, _ := io.ReadAll(trr.Body)
	if len(thumbBody) < 32 {
		t.Fatalf("thumb body too small: %d", len(thumbBody))
	}
	// Thumb must not be the raw full PNG bytes (browser gets preview, not full dump).
	if bytes.Equal(thumbBody, pngBytes) {
		t.Fatal("thumb served full PNG bytes; want jpeg preview")
	}

	// Full-res residual still available.
	fr := httptest.NewRequest(http.MethodGet, ImageFullURL(id), nil)
	frr := httptest.NewRecorder()
	mux.ServeHTTP(frr, fr)
	if frr.Code != http.StatusOK {
		t.Fatalf("full status %d", frr.Code)
	}
	fullBody, _ := io.ReadAll(frr.Body)
	if !bytes.Equal(fullBody, pngBytes) {
		t.Fatalf("full-res mismatch len %d vs %d", len(fullBody), len(pngBytes))
	}
}

// 🎯T224: legacy full-only store still lazy-generates thumb on GET.
func TestImageThumbLazyFromLegacyFull(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	id := "deadbeefcafebabe"
	pngBytes := validPNG(t, 100, 80)
	if err := os.MkdirAll(filepath.Join(dir, "images"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "images", id+".png"), pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	// No thumbs/ yet.
	tr := httptest.NewRequest(http.MethodGet, ImageThumbURL(id), nil)
	trr := httptest.NewRecorder()
	mux.ServeHTTP(trr, tr)
	if trr.Code != http.StatusOK {
		t.Fatalf("lazy thumb status %d", trr.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "images", "thumbs", id+".jpg")); err != nil {
		t.Fatalf("lazy thumb not written: %v", err)
	}
}

func TestImageMarkerAndURLs(t *testing.T) {
	id := "0123456789abcdef"
	if ImageMarker(id) != "[image: 0123456789abcdef]" {
		t.Fatal(ImageMarker(id))
	}
	if ImageThumbURL(id) != "/api/images/0123456789abcdef/thumb" {
		t.Fatal(ImageThumbURL(id))
	}
	if ImageFullURL(id) != "/api/images/0123456789abcdef" {
		t.Fatal(ImageFullURL(id))
	}
}
