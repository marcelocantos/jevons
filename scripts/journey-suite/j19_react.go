// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
)

// Every UI journey loads the packaged daemon. A missing React bundle is a
// product failure; starting Vite would hide precisely the packaging defect.
type j19ReactSurface struct {
	host string
	via  string
}

func (r *j19ReactSurface) stop() {} // The suite owns the daemon lifecycle.

func (s *suite) startJ19ReactSurface() (*j19ReactSurface, error) {
	if err := portguard.RefuseDaily(s.port); err != nil {
		return nil, err
	}
	body, err := fetchIsolateRoot(s.host)
	if err != nil {
		return nil, fmt.Errorf("j19 probe isolate GET /: %w", err)
	}
	if !j19HTMLIsVanilla(body) {
		return &j19ReactSurface{host: s.host, via: "isolate"}, nil
	}
	return nil, fmt.Errorf("packaged isolate GET / is not React; no Vite or vanilla fallback is permitted")
}

func fetchIsolateRoot(host string) ([]byte, error) {
	if err := refuseDailyHost(host); err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get("http://" + host + "/")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET / status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// Reject historical vanilla documents and unrecognised roots before a UI journey.
func j19HTMLIsVanilla(body []byte) bool {
	s := string(body)
	if strings.Contains(s, "DEPRECATED REFERENCE") {
		return true
	}
	if strings.Contains(s, "boot_sentinel.js") {
		return true
	}
	if strings.Contains(s, `id="root"`) || strings.Contains(s, "id='root'") {
		return false
	}
	return true
}

func refuseDailyHost(host string) error {
	_, portStr, err := net.SplitHostPort(host)
	if err != nil {
		if host == strconv.Itoa(portguard.DailyPort) {
			return portguard.RefuseDaily(portguard.DailyPort)
		}
		return nil
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return nil
	}
	return portguard.RefuseDaily(p)
}

func j19RepoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")), nil
}
