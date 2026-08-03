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

// FrontierDependent is a related target (id + short name) for tips and cards.
// Used for both incoming dependents (🎯T179) and outgoing depends_on (🎯T184).
type FrontierDependent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FrontierRow is one compact frontier table line.
// 🎯T181: acceptance/context/tags ship on the list payload so ID/name
// hover cards need no per-row detail round-trip.
// 🎯T184: full semantic card — depends_on, cost, attestation, dates, extra.
type FrontierRow struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Status      string              `json:"status"`
	Fanout      int                 `json:"fanout"`
	Dependents  []FrontierDependent `json:"dependents,omitempty"`
	DependsOn   []FrontierDependent `json:"depends_on,omitempty"`
	Value       float64             `json:"value,omitempty"`
	Cost        float64             `json:"cost,omitempty"`
	ActualCost  float64             `json:"actual_cost,omitempty"`
	Acceptance  []string            `json:"acceptance,omitempty"`
	Context     string              `json:"context,omitempty"`
	Tags        []string            `json:"tags,omitempty"`
	Attestation string              `json:"attestation,omitempty"`
	Origin      string              `json:"origin,omitempty"`
	Discovered  string              `json:"discovered,omitempty"`
	Achieved    string              `json:"achieved,omitempty"`
	// Extra carries non-canonical ledger keys as stringified values (🎯T184).
	Extra map[string]string `json:"extra,omitempty"`
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

// bullseyeTarget is the structured ledger fields used for frontier rows.
type bullseyeTarget struct {
	Name        string   `yaml:"name"`
	Status      string   `yaml:"status"`
	Value       float64  `yaml:"value"`
	Cost        float64  `yaml:"cost"`
	ActualCost  float64  `yaml:"actual_cost"`
	DependsOn   []string `yaml:"depends_on"`
	Acceptance  []string `yaml:"acceptance"`
	Context     string   `yaml:"context"`
	Tags        []string `yaml:"tags"`
	Attestation string   `yaml:"attestation"`
	Origin      string   `yaml:"origin"`
	Discovered  string   `yaml:"discovered"`
	Achieved    string   `yaml:"achieved"`
}

// knownBullseyeTargetKeys are fields mapped into FrontierRow columns; other
// target keys land in FrontierRow.Extra for key-value card display (🎯T184).
var knownBullseyeTargetKeys = map[string]bool{
	"name": true, "status": true, "value": true, "cost": true, "actual_cost": true,
	"depends_on": true, "acceptance": true, "context": true, "tags": true,
	"attestation": true, "origin": true, "discovered": true, "achieved": true,
}

// bullseyeLedger is the on-disk YAML shape (targets map only).
type bullseyeLedger struct {
	Targets map[string]bullseyeTarget `yaml:"targets"`
}

// bullseyeLedgerRaw re-reads targets as maps so Extra can capture unknown keys.
type bullseyeLedgerRaw struct {
	Targets map[string]map[string]any `yaml:"targets"`
}

func isActiveStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	return s == "identified" || s == "converging"
}

func isDoneStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	return s == "achieved" || s == "set_aside"
}

// stringifyYAMLValue flattens a YAML scalar/list for Extra key-value display.
func stringifyYAMLValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		// Prefer integer display when whole.
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(x))
		for _, el := range x {
			s := stringifyYAMLValue(el)
			if s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		// Compact JSON-ish for rare map extras.
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprint(x)
		}
		return string(b)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

// extractTargetExtra maps unknown ledger keys → string values (sorted keys).
func extractTargetExtra(raw map[string]any) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string)
	for k, v := range raw {
		key := strings.TrimSpace(k)
		if key == "" || knownBullseyeTargetKeys[key] {
			continue
		}
		s := stringifyYAMLValue(v)
		if s == "" {
			continue
		}
		out[key] = s
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// resolveDependsOn builds outgoing edges with names when the dep id is known.
func resolveDependsOn(ids []string, nameOf map[string]string) []FrontierDependent {
	if len(ids) == 0 {
		return []FrontierDependent{}
	}
	out := make([]FrontierDependent, 0, len(ids))
	seen := make(map[string]bool)
	for _, dep := range ids {
		dep = strings.TrimSpace(dep)
		if dep == "" || seen[dep] {
			continue
		}
		seen[dep] = true
		out = append(out, FrontierDependent{
			ID:   dep,
			Name: nameOf[dep],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return targetIDLess(out[i].ID, out[j].ID)
	})
	return out
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
	// Raw pass for Extra (unknown keys) — best-effort; ignore parse errors.
	extraByID := make(map[string]map[string]string)
	var rawDoc bullseyeLedgerRaw
	if err := yaml.Unmarshal(data, &rawDoc); err == nil && rawDoc.Targets != nil {
		for id, m := range rawDoc.Targets {
			if ex := extractTargetExtra(m); len(ex) > 0 {
				extraByID[id] = ex
			}
		}
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
		acc := t.Acceptance
		if acc == nil {
			acc = []string{}
		}
		tags := t.Tags
		if tags == nil {
			tags = []string{}
		}
		dependsOn := resolveDependsOn(t.DependsOn, nameOf)
		rows = append(rows, FrontierRow{
			ID:          id,
			Name:        t.Name,
			Status:      displayStatus(t.Status),
			Fanout:      len(deps),
			Dependents:  deps,
			DependsOn:   dependsOn,
			Value:       t.Value,
			Cost:        t.Cost,
			ActualCost:  t.ActualCost,
			Acceptance:  acc,
			Context:     strings.TrimSpace(t.Context),
			Tags:        tags,
			Attestation: strings.TrimSpace(t.Attestation),
			Origin:      strings.TrimSpace(t.Origin),
			Discovered:  strings.TrimSpace(t.Discovered),
			Achieved:    strings.TrimSpace(t.Achieved),
			Extra:       extraByID[id],
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

// GraphResponse is GET /api/frontier/graph JSON (🎯T185): entire unachieved
// dependency graph as Mermaid source for the Frontier Graph control.
type GraphResponse struct {
	Available bool   `json:"available"`
	Ledger    string `json:"ledger,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	Mermaid   string `json:"mermaid,omitempty"`
	NodeCount int    `json:"node_count,omitempty"`
	EdgeCount int    `json:"edge_count,omitempty"`
	Error     string `json:"error,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// mermaidSafeNodeID maps target ids to Mermaid node ids (T27.1 → T27_1).
func mermaidSafeNodeID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "node"
	}
	var b strings.Builder
	for _, r := range id {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "node"
	}
	// Mermaid identifiers should not start with a digit.
	if out[0] >= '0' && out[0] <= '9' {
		return "n_" + out
	}
	return out
}

// truncateMermaidLabel shortens a display name for dense graphs.
func truncateMermaidLabel(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	// rune-aware-ish: byte truncate is fine for ASCII-heavy ledger names.
	return s[:max-1] + "…"
}

// escapeMermaidLabel keeps quoted node text safe for Mermaid.
func escapeMermaidLabel(s string) string {
	s = strings.ReplaceAll(s, `"`, "'")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}

// computeActiveGraphMermaidFromLedger builds Mermaid for all unachieved
// (active) targets and depends_on edges among them (🎯T185). Achieved /
// set_aside nodes are omitted; edges to non-active deps are dropped.
func computeActiveGraphMermaidFromLedger(ledgerPath string) (mermaid string, nodeCount, edgeCount int, err error) {
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		return "", 0, 0, err
	}
	var doc bullseyeLedger
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", 0, 0, fmt.Errorf("parse ledger: %w", err)
	}
	if doc.Targets == nil || len(doc.Targets) == 0 {
		return "graph TD\n", 0, 0, nil
	}

	activeIDs := make([]string, 0)
	nameOf := make(map[string]string)
	active := make(map[string]bool)
	for id, t := range doc.Targets {
		if !isActiveStatus(t.Status) {
			continue
		}
		active[id] = true
		activeIDs = append(activeIDs, id)
		nameOf[id] = strings.TrimSpace(t.Name)
	}
	sort.Slice(activeIDs, func(i, j int) bool {
		return targetIDLess(activeIDs[i], activeIDs[j])
	})

	var b strings.Builder
	b.WriteString("graph TD\n")
	for _, id := range activeIDs {
		nid := mermaidSafeNodeID(id)
		label := nameOf[id]
		if label == "" {
			label = id
		} else {
			// Prefer "T185 · short name" for scanability in large graphs.
			label = id + " · " + truncateMermaidLabel(label, 36)
		}
		fmt.Fprintf(&b, "    %s[\"%s\"]\n", nid, escapeMermaidLabel(label))
	}

	// Edges: dependent -.->|needs| dep among active nodes only.
	type edge struct{ from, to string }
	edges := make([]edge, 0)
	seenEdge := make(map[string]bool)
	for _, id := range activeIDs {
		t := doc.Targets[id]
		from := mermaidSafeNodeID(id)
		for _, dep := range t.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" || !active[dep] {
				continue
			}
			to := mermaidSafeNodeID(dep)
			key := from + "\x00" + to
			if seenEdge[key] {
				continue
			}
			seenEdge[key] = true
			edges = append(edges, edge{from: from, to: to})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].from != edges[j].from {
			return edges[i].from < edges[j].from
		}
		return edges[i].to < edges[j].to
	})
	for _, e := range edges {
		fmt.Fprintf(&b, "    %s -.->|needs| %s\n", e.from, e.to)
	}
	return b.String(), len(activeIDs), len(edges), nil
}

// loadFrontierGraph discovers the ledger and returns active-graph Mermaid.
func loadFrontierGraph(cwd string) GraphResponse {
	now := time.Now().UTC().Format(time.RFC3339)
	resp := GraphResponse{
		Available: false,
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

	src, nodes, edges, err := computeActiveGraphMermaidFromLedger(ledger)
	if err != nil {
		// Fallback: bullseye CLI graph export (scope=active ≈ unachieved).
		out, cliErr := runBullseyeCLI("query", "--view", "graph", "--scope", "active", "--cwd", abs)
		if cliErr == nil && strings.TrimSpace(out) != "" {
			// Strip optional ```mermaid fences from CLI output.
			src = stripMermaidFenceCLI(out)
			if src != "" {
				resp.Mermaid = src
				resp.Available = true
				return resp
			}
		}
		resp.Error = fmt.Sprintf("read ledger: %v", err)
		return resp
	}
	resp.Mermaid = src
	resp.NodeCount = nodes
	resp.EdgeCount = edges
	resp.Available = true
	return resp
}

// stripMermaidFenceCLI peels ```mermaid wrappers from bullseye graph export.
func stripMermaidFenceCLI(text string) string {
	s := strings.TrimSpace(text)
	s = strings.TrimPrefix(s, "\ufeff")
	if strings.HasPrefix(s, "```") {
		// Drop first fence line.
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		} else {
			return ""
		}
		s = strings.TrimSpace(s)
		if strings.HasSuffix(s, "```") {
			s = strings.TrimSpace(s[:len(s)-3])
		}
	}
	return strings.TrimSpace(s)
}

// handleFrontierGraph serves GET /api/frontier/graph — unachieved Mermaid (🎯T185).
func (s *Server) handleFrontierGraph(w http.ResponseWriter, r *http.Request) {
	cwd := s.frontierCwdOr(r.URL.Query().Get("cwd"))
	resp := loadFrontierGraph(cwd)
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
