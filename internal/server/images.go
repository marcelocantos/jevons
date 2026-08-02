// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// registerImageRoutes mounts 🎯T76 image upload/serve under /api/images.
func (s *Server) registerImageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/images", s.handleImageUpload)
	mux.HandleFunc("GET /api/images/{id}", s.handleImageGet)
}

func (s *Server) imagesDir() string {
	return filepath.Join(s.stateDir, "images")
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
	slog.Info("image uploaded", "id", id, "bytes", n, "path", path)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":   id,
		"ext":  ext,
		"url":  "/api/images/" + id,
		"path": path,
		// Marker the overseer / journal understand (🎯T76).
		"marker": fmt.Sprintf("[image: %s]", id),
	})
}

func (s *Server) handleImageGet(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	id = strings.TrimSuffix(id, filepath.Ext(id))
	if id == "" || strings.Contains(id, "/") || strings.Contains(id, "..") {
		http.NotFound(w, r)
		return
	}
	dir := s.imagesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var path string
	var ext string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, id+".") || name == id {
			path = filepath.Join(dir, name)
			ext = filepath.Ext(name)
			break
		}
	}
	if path == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentTypeForExt(ext))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, path)
}

func newImageID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
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

// ImageMarkerRE matches [image: <id>] in chat text (exported for tests).
const ImageMarkerPrefix = "[image: "
