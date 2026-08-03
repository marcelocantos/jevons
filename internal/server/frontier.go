// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// 🎯T131: live bullseye frontier for the RHS bottom pane.
//
// Path discovery always goes through the bullseye CLI (`open`) — never a
// hard-coded bullseye.yaml path (in-repo vs external shadow). Once the
// ledger path is known, rows are built by reading that file (frontier
// graph: active targets with deps satisfied). Live refresh: fsnotify on
// the resolved ledger + client poll fallback. No Bullseye WebSocket.

// FrontierDependent is an active target that lists this row's id in depends_on (🎯T179).
type FrontierDependent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FrontierRow is one compact frontier table line.
type FrontierRow struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Status     string              `json:"status"`
	Fanout     int                 `json:"fanout"`
	Dependents []FrontierDependent `json:"dependents,omitempty"`
	Value      float64             `json:"value,omitempty"`
}

// FrontierResponse is GET /api/frontier JSON.
type FrontierResponse struct {
	Available bool          `json:"available"`
	Ledger    string        `json:"ledger,omitempty"`
	Cwd       string        `json:"cwd,omitempty"`
	Targets   []FrontierRow `json:"targets"`
	Error     string        `json:"error,omitempty"`
	UpdatedAt string        `json:"updated_at,omitempty"`
}

// runBullseyeCLI shells out to bullseye on PATH. Tests override.
var runBullseyeCLI = func(args ...string) (string, error) {
	cmd := exec.Command("bullseye", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// fileLineRe extracts "File: /path/to/bullseye.yaml" from open/query output.
var fileLineRe = regexp.MustCompile(`(?m)^File:\s*(.+)$`)

// frontierLineRe matches bullseye query --view frontier lines (fallback).
//
//	🎯T27.1 The provider contract …  [Converging] — fanout=3
var frontierLineRe = regexp.MustCompile(
	`(?m)^(?:🎯)?(T[\d.]+)\s+(.+?)\s+\[([^\]]+)\](?:\s*(?:—|--)\s*fanout=(\d+))?`,
)

// parseBullseyeLedgerPath returns the ledger path from bullseye open/query text.
func parseBullseyeLedgerPath(out string) string {
	m := fileLineRe.FindStringSubmatch(out)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// parseFrontierRows extracts table rows from bullseye frontier text (fallback).
func parseFrontierRows(out string) []FrontierRow {
	matches := frontierLineRe.FindAllStringSubmatch(out, -1)
	if len(matches) == 0 {
		return []FrontierRow{}
	}
	rows := make([]FrontierRow, 0, len(matches))
	for _, m := range matches {
		fanout := 0
		if m[4] != "" {
			fanout, _ = strconv.Atoi(m[4])
		}
		rows = append(rows, FrontierRow{
			ID:     m[1],
			Name:   strings.TrimSpace(m[2]),
			Status: strings.TrimSpace(m[3]),
			Fanout: fanout,
		})
	}
	return rows
}

// isBullseyeNotInitialized reports the calm empty state from bullseye open.
func isBullseyeNotInitialized(out string) bool {
	return strings.Contains(out, "code=not_initialized") ||
		strings.Contains(out, "no bullseye.yaml found")
}

// bullseyeTarget is the subset of ledger fields needed for frontier rows.
type bullseyeTarget struct {
	Name      string   `yaml:"name"`
	Status    string   `yaml:"status"`
	Value     float64  `yaml:"value"`
	DependsOn []string `yaml:"depends_on"`
}

// bullseyeLedger is the on-disk YAML shape (targets map only).
type bullseyeLedger struct {
	Targets map[string]bullseyeTarget `yaml:"targets"`
}

func isActiveStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	return s == "identified" || s == "converging"
}

func isDoneStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	return s == "achieved" || s == "set_aside"
}

// computeFrontierFromLedger reads a discovered ledger path and builds frontier
// rows: active targets whose depends_on are all done (or absent). Fanout is
// the count of active dependents that list this id. Ordered by fanout desc, id.
func computeFrontierFromLedger(ledgerPath string) ([]FrontierRow, error) {
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		return nil, err
	}
	var doc bullseyeLedger
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse ledger: %w", err)
	}
	if doc.Targets == nil {
		return []FrontierRow{}, nil
	}

	// Precompute active set and reverse edges for fanout / dependents (🎯T179).
	active := make(map[string]bool)
	statusOf := make(map[string]string)
	nameOf := make(map[string]string)
	for id, t := range doc.Targets {
		statusOf[id] = t.Status
		nameOf[id] = t.Name
		if isActiveStatus(t.Status) {
			active[id] = true
		}
	}
	// dependents[depID] = active targets that list depID in depends_on
	dependents := make(map[string][]FrontierDependent)
	for id, t := range doc.Targets {
		if !active[id] {
			continue
		}
		for _, dep := range t.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}
			dependents[dep] = append(dependents[dep], FrontierDependent{
				ID:   id,
				Name: nameOf[id],
			})
		}
	}
	// Stable order for tip lists.
	for dep := range dependents {
		sort.Slice(dependents[dep], func(i, j int) bool {
			return targetIDLess(dependents[dep][i].ID, dependents[dep][j].ID)
		})
	}

	rows := make([]FrontierRow, 0)
	for id, t := range doc.Targets {
		if !active[id] {
			continue
		}
		// All depends_on must be done (or missing = treat as blocking if unknown).
		ready := true
		for _, dep := range t.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}
			st, ok := statusOf[dep]
			if !ok {
				// Unknown dep: treat as not done (conservative).
				ready = false
				break
			}
			if !isDoneStatus(st) {
				ready = false
				break
			}
		}
		if !ready {
			continue
		}
		deps := dependents[id]
		if deps == nil {
			deps = []FrontierDependent{}
		}
		rows = append(rows, FrontierRow{
			ID:         id,
			Name:       t.Name,
			Status:     displayStatus(t.Status),
			Fanout:     len(deps),
			Dependents: deps,
			Value:      t.Value,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Fanout != rows[j].Fanout {
			return rows[i].Fanout > rows[j].Fanout
		}
		return targetIDLess(rows[i].ID, rows[j].ID)
	})
	return rows, nil
}

func displayStatus(status string) string {
	s := strings.TrimSpace(status)
	if s == "" {
		return ""
	}
	// Title-case single token (identified → Identified).
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

// targetIDLess orders T1 < T2 < T10 < T1.1 roughly by numeric segments.
func targetIDLess(a, b string) bool {
	return a < b
}

// SetFrontierCwd sets the primary workdir used to discover the project ledger
// via bullseye open (not a hard-coded yaml path).
func (s *Server) SetFrontierCwd(cwd string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frontierCwd = strings.TrimSpace(cwd)
}

// frontierCwdOr returns query cwd, configured frontier cwd, or process cwd.
func (s *Server) frontierCwdOr(query string) string {
	q := strings.TrimSpace(query)
	if q != "" {
		return q
	}
	s.mu.RLock()
	cwd := s.frontierCwd
	s.mu.RUnlock()
	if cwd != "" {
		return cwd
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// discoverLedgerPath asks bullseye where the ledger is for cwd.
func discoverLedgerPath(cwd string) (ledger string, notInit bool, err error) {
	openOut, openErr := runBullseyeCLI("open", "--cwd", cwd)
	if isBullseyeNotInitialized(openOut) {
		return "", true, nil
	}
	ledger = parseBullseyeLedgerPath(openOut)
	if ledger != "" {
		return ledger, false, nil
	}
	if openErr != nil {
		return "", false, fmt.Errorf("bullseye open: %v — %s", openErr, firstLine(openOut))
	}
	return "", false, fmt.Errorf("bullseye open did not report a ledger path")
}

// loadFrontier discovers the ledger via bullseye and builds the frontier table.
func loadFrontier(cwd string) FrontierResponse {
	now := time.Now().UTC().Format(time.RFC3339)
	resp := FrontierResponse{
		Available: false,
		Targets:   []FrontierRow{},
		UpdatedAt: now,
	}
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		resp.Error = "no workdir for bullseye discovery"
		return resp
	}
	if strings.HasPrefix(cwd, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			cwd = filepath.Join(home, cwd[2:])
		}
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		resp.Error = fmt.Sprintf("cwd: %v", err)
		return resp
	}
	resp.Cwd = abs

	ledger, notInit, err := discoverLedgerPath(abs)
	if notInit {
		resp.Error = "no bullseye ledger for this workdir"
		return resp
	}
	if err != nil {
		resp.Error = err.Error()
		return resp
	}
	resp.Ledger = ledger

	rows, err := computeFrontierFromLedger(ledger)
	if err != nil {
		// Fallback: try CLI frontier text if file parse fails.
		frontOut, frontErr := runBullseyeCLI("query", "--view", "frontier", "--cwd", abs)
		if frontErr == nil || strings.TrimSpace(frontOut) != "" {
			rows = parseFrontierRows(frontOut)
			if len(rows) > 0 {
				resp.Targets = rows
				resp.Available = true
				return resp
			}
		}
		resp.Error = fmt.Sprintf("read ledger: %v", err)
		return resp
	}
	resp.Targets = rows
	resp.Available = true
	return resp
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// handleFrontier serves GET /api/frontier — live frontier table for RHS (🎯T131).
func (s *Server) handleFrontier(w http.ResponseWriter, r *http.Request) {
	cwd := s.frontierCwdOr(r.URL.Query().Get("cwd"))
	resp := loadFrontier(cwd)
	if resp.Available && resp.Ledger != "" {
		s.ensureFrontierWatch(resp.Ledger)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// NotifyFrontierChanged pushes a live frame so the RHS frontier table refreshes
// without waiting on poll (🎯T131). Safe from any goroutine.
func (s *Server) NotifyFrontierChanged() {
	payload, err := json.Marshal(map[string]any{"type": "frontier_changed"})
	if err != nil {
		return
	}
	s.broadcastChatLive(string(payload))
}

// ensureFrontierWatch starts (or rebinds) fsnotify on the ledger file path.
func (s *Server) ensureFrontierWatch(ledger string) {
	ledger = filepath.Clean(ledger)
	s.mu.Lock()
	if s.frontierWatchPath == ledger && s.frontierWatchCancel != nil {
		s.mu.Unlock()
		return
	}
	if s.frontierWatchCancel != nil {
		s.frontierWatchCancel()
		s.frontierWatchCancel = nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.frontierWatchCancel = cancel
	s.frontierWatchPath = ledger
	s.mu.Unlock()
	go s.runFrontierWatch(ctx, ledger)
}

// WatchFrontierFile watches a known ledger path (optional explicit start).
func (s *Server) WatchFrontierFile(path string) (stop func()) {
	if path == "" {
		return func() {}
	}
	s.ensureFrontierWatch(path)
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.frontierWatchCancel != nil {
			s.frontierWatchCancel()
			s.frontierWatchCancel = nil
			s.frontierWatchPath = ""
		}
	}
}

func (s *Server) runFrontierWatch(ctx context.Context, path string) {
	if path == "" {
		return
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("frontier watch: fsnotify unavailable", "err", err)
		return
	}
	defer w.Close()

	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if err := w.Add(dir); err != nil {
		slog.Warn("frontier watch: add dir failed", "dir", dir, "err", err)
		return
	}
	slog.Info("frontier watch started", "path", path)

	var (
		timer *time.Timer
		mu    sync.Mutex
	)
	fire := func() {
		s.NotifyFrontierChanged()
	}
	schedule := func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(80*time.Millisecond, fire)
	}

	for {
		select {
		case <-ctx.Done():
			mu.Lock()
			if timer != nil {
				timer.Stop()
			}
			mu.Unlock()
			return
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			slog.Warn("frontier watch error", "err", err)
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if filepath.Base(ev.Name) != base {
				continue
			}
			if ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) ||
				ev.Has(fsnotify.Rename) || ev.Has(fsnotify.Remove) {
				schedule()
			}
		}
	}
}
