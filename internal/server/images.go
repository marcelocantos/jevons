// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	// image/gif, image/jpeg, image/png register decoders via their imports above
	// (jpeg/png also used for encode). GIF is decode-only here.
	_ "image/gif"

	"golang.org/x/image/draw"
)

// ImageThumbMaxEdge is the longest side (px) for browser-delivered thumbs (🎯T224).
// Full-res stays on disk; clients render compact previews only.
const ImageThumbMaxEdge = 320

// ImageThumbJPEGQuality is the dual-encode JPEG quality for thumbs (🎯T257).
// Moderately high (~88–90) so photo-like previews stay sharp while still
// beating PNG on continuous-tone imagery.
const ImageThumbJPEGQuality = 89

// ImageThumbJPEGSizeFactor is k in the dual-encode size rule (🎯T257):
// serve JPEG only when jpeg_bytes * k < png_bytes. Default k=2 means JPEG
// must be at least ~2× smaller than the lossless PNG to win; otherwise PNG
// is served (UI/screenshots, flat graphics). Alpha always forces PNG.
const ImageThumbJPEGSizeFactor = 2

// ImageMarkerPrefix is the journal/chat marker for a stored image (🎯T76/T224).
const ImageMarkerPrefix = "[image: "

// registerImageRoutes mounts image upload/serve under /api/images (🎯T76/T224).
// Full-res residual: GET /api/images/{id}
// Browser default:   GET /api/images/{id}/thumb
func (s *Server) registerImageRoutes(mux router) {
	mux.HandleFunc("POST /api/images", s.handleImageUpload)
	mux.HandleFunc("GET /api/images/{id}/thumb", s.handleImageThumbGet)
	mux.HandleFunc("GET /api/images/{id}", s.handleImageGet)
}

func (s *Server) imagesDir() string {
	return filepath.Join(s.stateDir, "images")
}

func (s *Server) thumbsDir() string {
	return filepath.Join(s.imagesDir(), "thumbs")
}

func (s *Server) handleImageUpload(w http.ResponseWriter, r *http.Request) {
	if rejectCrossSite(w, r) {
		return
	}
	if err := r.ParseMultipartForm(12 << 20); err != nil { // 12 MiB
		http.Error(w, `{"error":"invalid multipart form"}`, http.StatusBadRequest)
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"file field required"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	if ext == "" {
		ext = extFromContentType(hdr.Header.Get("Content-Type"))
	}
	if !allowedImageExt(ext) {
		http.Error(w, `{"error":"unsupported image type"}`, http.StatusBadRequest)
		return
	}

	id, err := newImageID()
	if err != nil {
		http.Error(w, `{"error":"id mint failed"}`, http.StatusInternalServerError)
		return
	}
	if err := os.MkdirAll(s.imagesDir(), 0o755); err != nil {
		http.Error(w, `{"error":"mkdir failed"}`, http.StatusInternalServerError)
		return
	}
	name := id + ext
	path := filepath.Join(s.imagesDir(), name)
	dst, err := os.Create(path)
	if err != nil {
		http.Error(w, `{"error":"create failed"}`, http.StatusInternalServerError)
		return
	}
	n, copyErr := io.Copy(dst, io.LimitReader(file, 10<<20))
	_ = dst.Close()
	if copyErr != nil {
		_ = os.Remove(path)
		http.Error(w, `{"error":"write failed"}`, http.StatusInternalServerError)
		return
	}

	// Best-effort thumb for browser hydrate (🎯T224). Failure is non-fatal:
	// thumb GET falls back to full-res or generates on demand.
	thumbOK := false
	if err := s.writeThumbFromFull(id, path); err != nil {
		slog.Debug("image thumb deferred", "id", id, "err", err)
	} else {
		thumbOK = true
	}
	slog.Info("image uploaded", "id", id, "bytes", n, "path", path, "thumb", thumbOK)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":        id,
		"ext":       ext,
		"url":       ImageFullURL(id),  // full-res residual
		"thumb_url": ImageThumbURL(id), // browser default (🎯T224)
		"path":      path,
		// Marker the overseer / journal understand (🎯T76/T224).
		"marker": ImageMarker(id),
	})
}

func (s *Server) handleImageGet(w http.ResponseWriter, r *http.Request) {
	id := sanitizeImageID(r.PathValue("id"))
	if id == "" {
		http.NotFound(w, r)
		return
	}
	path, ext, err := s.findFullImage(id)
	if err != nil || path == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentTypeForExt(ext))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, path)
}

func (s *Server) handleImageThumbGet(w http.ResponseWriter, r *http.Request) {
	id := sanitizeImageID(r.PathValue("id"))
	if id == "" {
		http.NotFound(w, r)
		return
	}
	if path, ct, ok := s.findThumb(id); ok {
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "private, max-age=86400")
		http.ServeFile(w, r, path)
		return
	}
	// Lazy generate from full-res (legacy uploads before T224).
	full, _, err := s.findFullImage(id)
	if err != nil || full == "" {
		http.NotFound(w, r)
		return
	}
	if err := s.writeThumbFromFull(id, full); err != nil {
		// Undecodable (e.g. webp without decoder): fall back to full file.
		slog.Debug("image thumb fallback full", "id", id, "err", err)
		w.Header().Set("Content-Type", contentTypeForExt(filepath.Ext(full)))
		w.Header().Set("Cache-Control", "private, max-age=86400")
		http.ServeFile(w, r, full)
		return
	}
	path, ct, ok := s.findThumb(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, path)
}

// findThumb locates an on-disk thumb for id (.jpg or .png after 🎯T257 dual-encode).
// Prefer JPEG if both exist (legacy dual write should not leave both).
func (s *Server) findThumb(id string) (path, contentType string, ok bool) {
	for _, ext := range []string{".jpg", ".png"} {
		p := filepath.Join(s.thumbsDir(), id+ext)
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			return p, contentTypeForExt(ext), true
		}
	}
	return "", "", false
}

// findFullImage locates the stored full-res file for id. Skips thumbs/ subdir.
func (s *Server) findFullImage(id string) (path, ext string, err error) {
	dir := s.imagesDir()
	for _, e := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp"} {
		p := filepath.Join(dir, id+e)
		if st, e2 := os.Stat(p); e2 == nil && !st.IsDir() {
			return p, e, nil
		}
	}
	// Legacy: bare id or unexpected extension via dir scan (never under thumbs/).
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == id || strings.HasPrefix(name, id+".") {
			if strings.Contains(name, ".thumb.") || strings.HasSuffix(name, "_thumb.jpg") {
				continue
			}
			return filepath.Join(dir, name), filepath.Ext(name), nil
		}
	}
	return "", "", os.ErrNotExist
}

func (s *Server) writeThumbFromFull(id, fullPath string) error {
	f, err := os.Open(fullPath)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	thumb := resizeMaxEdge(img, ImageThumbMaxEdge)
	data, ext, err := chooseThumbFormat(thumb)
	if err != nil {
		return err
	}
	return s.writeThumbBytes(id, data, ext)
}

// chooseThumbFormat dual-encodes PNG + JPEG after resample (🎯T257).
// Alpha/transparency → PNG only. Otherwise JPEG wins only when
// jpeg_bytes * ImageThumbJPEGSizeFactor < png_bytes; else PNG.
func chooseThumbFormat(img image.Image) (data []byte, ext string, err error) {
	if imageHasAlpha(img) {
		b, err := encodeThumbPNG(img)
		if err != nil {
			return nil, "", err
		}
		return b, ".png", nil
	}
	pngB, err := encodeThumbPNG(img)
	if err != nil {
		return nil, "", err
	}
	jpegB, err := encodeThumbJPEG(img)
	if err != nil {
		// JPEG encode failure is rare for opaque RGB; keep PNG.
		return pngB, ".png", nil
	}
	if jpegThumbWins(len(jpegB), len(pngB)) {
		return jpegB, ".jpg", nil
	}
	return pngB, ".png", nil
}

// jpegThumbWins reports whether JPEG should be served under the dual-encode
// size rule: jpeg_bytes * k < png_bytes (k = ImageThumbJPEGSizeFactor).
func jpegThumbWins(jpegBytes, pngBytes int) bool {
	if jpegBytes <= 0 || pngBytes <= 0 {
		return false
	}
	return jpegBytes*ImageThumbJPEGSizeFactor < pngBytes
}

func encodeThumbPNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeThumbJPEG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: ImageThumbJPEGQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// imageHasAlpha is true when any pixel is not fully opaque — forces PNG (🎯T257).
func imageHasAlpha(img image.Image) bool {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a < 0xffff {
				return true
			}
		}
	}
	return false
}

// writeThumbBytes atomically writes the chosen thumb bytes and removes the
// alternate extension so GET is unambiguous (.jpg vs .png).
func (s *Server) writeThumbBytes(id string, data []byte, ext string) error {
	if err := os.MkdirAll(s.thumbsDir(), 0o755); err != nil {
		return err
	}
	path := filepath.Join(s.thumbsDir(), id+ext)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	alt := ".png"
	if ext == ".png" {
		alt = ".jpg"
	}
	_ = os.Remove(filepath.Join(s.thumbsDir(), id+alt))
	return nil
}

// resizeMaxEdge returns img scaled so max(width,height) <= maxEdge.
// Catmull-Rom (cubic) resampling — not nearest-neighbour (🎯T256).
// Existing on-disk thumbs under images/thumbs/ stay old until re-upload
// or lazy re-generation after delete; new uploads and lazy GET regenerate.
func resizeMaxEdge(img image.Image, maxEdge int) image.Image {
	if maxEdge <= 0 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return img
	}
	if w <= maxEdge && h <= maxEdge {
		return img
	}
	scale := float64(maxEdge) / float64(w)
	if h > w {
		scale = float64(maxEdge) / float64(h)
	}
	nw := int(float64(w)*scale + 0.5)
	nh := int(float64(h)*scale + 0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

func sanitizeImageID(raw string) string {
	id := strings.TrimSpace(raw)
	id = strings.TrimSuffix(id, filepath.Ext(id))
	if id == "" || strings.Contains(id, "/") || strings.Contains(id, "..") ||
		strings.Contains(id, "\\") {
		return ""
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return ""
		}
	}
	return strings.ToLower(id)
}

func newImageID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// ImageMarker returns the journal text marker for a stored image id.
func ImageMarker(id string) string {
	return fmt.Sprintf("[image: %s]", id)
}

// ImageThumbURL is the browser-facing compact preview path (🎯T224).
func ImageThumbURL(id string) string {
	return "/api/images/" + id + "/thumb"
}

// ImageFullURL is the full-res residual path.
func ImageFullURL(id string) string {
	return "/api/images/" + id
}

func allowedImageExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	default:
		return false
	}
}

func extFromContentType(ct string) string {
	ct = strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
	switch ct {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

func contentTypeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}
