// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
)

// 🎯T540.2: isolate GET / is React when ui/dist exists. Without a
// dist, startJ19ReactSurface starts a :5173-style Vite proxy against
// the isolate (ephemeral port, never :13705). Dual-path residual.
// Do not add a second connect-tail journey.

type j19ReactSurface struct {
	host string // host:port Playwright loads (React)
	via  string // "isolate" or "vite-proxy"
	cmd  *exec.Cmd
}

func (r *j19ReactSurface) stop() {
	if r == nil || r.cmd == nil || r.cmd.Process == nil {
		return
	}
	_ = r.cmd.Process.Kill()
	_, _ = r.cmd.Process.Wait()
	r.cmd = nil
}

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
	// 🎯T540.2 dual-path residual: isolate GET / has no ui/dist.
	// Load React through a Vite proxy aimed at this isolate, not daily.
	return startJ19ViteProxy(s.host)
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

// j19HTMLIsVanilla is the T540.2 residual detector. Vanilla web/ carries
// the deprecation banner / boot_sentinel; React ui/ mounts #root.
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

func startJ19ViteProxy(isolateHost string) (*j19ReactSurface, error) {
	if err := refuseDailyHost(isolateHost); err != nil {
		return nil, err
	}
	uiPort, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("j19 react proxy port: %w", err)
	}
	if err := portguard.RefuseDaily(uiPort); err != nil {
		return nil, err
	}
	root, err := j19RepoRoot()
	if err != nil {
		return nil, err
	}
	uiRoot := filepath.Join(root, "ui")
	viteBin := filepath.Join(uiRoot, "node_modules", ".bin", "vite")
	if st, err := os.Stat(viteBin); err != nil || st.IsDir() {
		return nil, fmt.Errorf("j19 react proxy: %s missing — npm install in ui/", viteBin)
	}
	cfg, err := j19ViteConfig()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(viteBin,
		"--config", cfg,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(uiPort),
		"--strictPort",
	)
	cmd.Dir = uiRoot
	cmd.Env = append(os.Environ(), "J19_ISOLATE="+isolateHost, "REACT_ISOLATE="+isolateHost)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("j19 react proxy start: %w", err)
	}
	surf := &j19ReactSurface{
		host: fmt.Sprintf("127.0.0.1:%d", uiPort),
		via:  "vite-proxy",
		cmd:  cmd,
	}
	if err := waitJ19ReactReady(surf.host, 45*time.Second); err != nil {
		surf.stop()
		return nil, fmt.Errorf("j19 react proxy ready: %w", err)
	}
	fmt.Printf("j19 React surface via=%s host=%s isolate=%s (T540.2 dual-path: isolate GET / had no ui/dist)\n",
		surf.via, surf.host, isolateHost)
	return surf, nil
}

func waitJ19ReactReady(host string, d time.Duration) error {
	if err := refuseDailyHost(host); err != nil {
		return err
	}
	deadline := time.Now().Add(d)
	client := &http.Client{Timeout: 2 * time.Second}
	var last error
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://" + host + "/")
		if err != nil {
			last = err
			time.Sleep(250 * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK && !j19HTMLIsVanilla(body) {
			return nil
		}
		last = fmt.Errorf("status %d vanilla=%v", resp.StatusCode, j19HTMLIsVanilla(body))
		time.Sleep(250 * time.Millisecond)
	}
	if last == nil {
		last = fmt.Errorf("timeout")
	}
	return last
}

func j19RepoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")), nil
}

func j19ViteConfig() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	cfg := filepath.Join(filepath.Dir(file), "j19_vite.config.mjs")
	if st, err := os.Stat(cfg); err != nil || st.IsDir() {
		return "", fmt.Errorf("j19_vite.config.mjs missing: %s", cfg)
	}
	return cfg, nil
}
