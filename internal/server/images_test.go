// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/chatlog"
)

// validPNG builds a real w×h solid-colour PNG (screenshot-like; dual-encode → PNG).
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
	return encodePNG(t, img)
}

// photoLikePNG is continuous-tone noise so JPEG dual-encode wins by size (🎯T257).
func photoLikePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(42))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Smooth-ish gradient + noise → continuous tone, poor PNG compress.
			base := uint8((x*3 + y*2) % 200)
			n := uint8(rng.Intn(56))
			i := (y*img.Stride) + x*4
			img.Pix[i+0] = base + n
			img.Pix[i+1] = base/2 + n
			img.Pix[i+2] = 80 + n/2
			img.Pix[i+3] = 255
		}
	}
	return encodePNG(t, img)
}

// alphaPNG has partial transparency → dual-encode must force PNG (🎯T257).
func alphaPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(7))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*img.Stride) + x*4
			img.Pix[i+0] = uint8(rng.Intn(256))
			img.Pix[i+1] = uint8(rng.Intn(256))
			img.Pix[i+2] = uint8(rng.Intn(256))
			// Checkerboard alpha so hasAlpha is true even after resize.
			if (x+y)%2 == 0 {
				img.Pix[i+3] = 128
			} else {
				img.Pix[i+3] = 255
			}
		}
	}
	return encodePNG(t, img)
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// findThumbOnDisk returns path + ext for whichever dual-encode format was written.
func findThumbOnDisk(t *testing.T, stateDir, id string) (path, ext string) {
	t.Helper()
	for _, e := range []string{".jpg", ".png"} {
		p := filepath.Join(stateDir, "images", "thumbs", id+e)
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			return p, e
		}
	}
	t.Fatalf("no thumb on disk for id=%s under %s", id, stateDir)
	return "", ""
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

	// Thumb file on disk under images/thumbs/ (PNG or JPEG after 🎯T257 dual-encode).
	thumbPath, _ := findThumbOnDisk(t, dir, id)
	_ = thumbPath

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

	// Thumb endpoint serves compact preview (PNG or JPEG; Content-Type matches file).
	tr := httptest.NewRequest(http.MethodGet, ImageThumbURL(id), nil)
	trr := httptest.NewRecorder()
	mux.ServeHTTP(trr, tr)
	if trr.Code != http.StatusOK {
		t.Fatalf("thumb status %d: %s", trr.Code, trr.Body.String())
	}
	ct := trr.Header().Get("Content-Type")
	if !strings.Contains(ct, "image/jpeg") && !strings.Contains(ct, "image/png") {
		t.Fatalf("thumb content-type %q want image/jpeg or image/png", ct)
	}
	thumbBody, _ := io.ReadAll(trr.Body)
	if len(thumbBody) < 32 {
		t.Fatalf("thumb body too small: %d", len(thumbBody))
	}
	// Thumb must not be the raw full original bytes (browser gets preview).
	if bytes.Equal(thumbBody, pngBytes) {
		t.Fatal("thumb served full original bytes; want dual-encode preview")
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
	findThumbOnDisk(t, dir, id)
}

// 🎯T257: photo-like continuous tone → JPEG by size rule (jpeg*k < png).
func TestThumbPhotoLikePrefersJPEG(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	resp := postImage(t, mux, photoLikePNG(t, 400, 300))
	id, _ := resp["id"].(string)
	if id == "" {
		t.Fatalf("no id: %+v", resp)
	}
	_, ext := findThumbOnDisk(t, dir, id)
	if ext != ".jpg" {
		t.Fatalf("photo-like thumb ext=%s want .jpg", ext)
	}
	tr := httptest.NewRequest(http.MethodGet, ImageThumbURL(id), nil)
	trr := httptest.NewRecorder()
	mux.ServeHTTP(trr, tr)
	if trr.Code != http.StatusOK {
		t.Fatalf("thumb status %d", trr.Code)
	}
	if ct := trr.Header().Get("Content-Type"); !strings.Contains(ct, "image/jpeg") {
		t.Fatalf("content-type %q want image/jpeg", ct)
	}
	// [image: id] hydrate path still resolves (marker + thumb_url).
	if resp["marker"] != ImageMarker(id) {
		t.Fatalf("marker: %+v", resp)
	}
	if resp["thumb_url"] != ImageThumbURL(id) {
		t.Fatalf("thumb_url: %+v", resp)
	}
}

// 🎯T257: screenshot-like flat UI → PNG (JPEG not substantially smaller).
func TestThumbScreenshotLikePrefersPNG(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	// Solid / few-colour fixture compresses well as PNG.
	resp := postImage(t, mux, validPNG(t, 400, 300))
	id, _ := resp["id"].(string)
	_, ext := findThumbOnDisk(t, dir, id)
	if ext != ".png" {
		t.Fatalf("screenshot-like thumb ext=%s want .png", ext)
	}
	tr := httptest.NewRequest(http.MethodGet, ImageThumbURL(id), nil)
	trr := httptest.NewRecorder()
	mux.ServeHTTP(trr, tr)
	if trr.Code != http.StatusOK {
		t.Fatalf("thumb status %d", trr.Code)
	}
	if ct := trr.Header().Get("Content-Type"); !strings.Contains(ct, "image/png") {
		t.Fatalf("content-type %q want image/png", ct)
	}
	if resp["marker"] != ImageMarker(id) {
		t.Fatalf("marker: %+v", resp)
	}
}

// 🎯T257: any alpha forces PNG even when photo-noise would favour JPEG.
func TestThumbAlphaForcesPNG(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	resp := postImage(t, mux, alphaPNG(t, 200, 160))
	id, _ := resp["id"].(string)
	_, ext := findThumbOnDisk(t, dir, id)
	if ext != ".png" {
		t.Fatalf("alpha thumb ext=%s want .png", ext)
	}
	tr := httptest.NewRequest(http.MethodGet, ImageThumbURL(id), nil)
	trr := httptest.NewRecorder()
	mux.ServeHTTP(trr, tr)
	if ct := trr.Header().Get("Content-Type"); !strings.Contains(ct, "image/png") {
		t.Fatalf("content-type %q want image/png", ct)
	}
}

func TestJPEGThumbWinsRule(t *testing.T) {
	// k=2: JPEG must be at least 2× smaller than PNG.
	if !jpegThumbWins(100, 201) {
		t.Fatal("100*2 < 201 → JPEG should win")
	}
	if jpegThumbWins(100, 200) {
		t.Fatal("100*2 == 200 → JPEG should not win (strict <)")
	}
	if jpegThumbWins(100, 199) {
		t.Fatal("100*2 > 199 → JPEG should not win")
	}
	if jpegThumbWins(0, 1000) || jpegThumbWins(100, 0) {
		t.Fatal("zero sizes must not prefer JPEG")
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

// 🎯T256: downscale must not be pure nearest-neighbour.
// Sharp black|white edge: NN keeps only 0/255; Catmull-Rom/bilinear
// produces intermediate samples near the seam.
func TestResizeMaxEdgeNotNearestNeighbour(t *testing.T) {
	const w, h = 640, 40
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2 {
				src.Set(x, y, color.RGBA{0, 0, 0, 255})
			} else {
				src.Set(x, y, color.RGBA{255, 255, 255, 255})
			}
		}
	}
	out := resizeMaxEdge(src, 80)
	ob := out.Bounds()
	if ob.Dx() != 80 || ob.Dy() != 5 {
		t.Fatalf("size got %dx%d want 80x5", ob.Dx(), ob.Dy())
	}
	inter := 0
	for y := ob.Min.Y; y < ob.Max.Y; y++ {
		for x := ob.Min.X; x < ob.Max.X; x++ {
			r, _, _, _ := out.At(x, y).RGBA()
			lv := int(r >> 8)
			if lv > 8 && lv < 247 {
				inter++
			}
		}
	}
	if inter == 0 {
		t.Fatal("downscale produced only pure black/white; want intermediate samples (not nearest-neighbour)")
	}
}

func TestResizeMaxEdgeNoOpWhenWithinMax(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 80))
	out := resizeMaxEdge(src, ImageThumbMaxEdge)
	if out.Bounds() != src.Bounds() {
		t.Fatalf("expected unchanged bounds, got %v", out.Bounds())
	}
}
