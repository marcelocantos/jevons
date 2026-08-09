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

// targetIDLess orders bullseye-style ids by natural/version segments (🎯T199).
// Digit runs compare as integers; non-digit runs are byte-wise.
// Example: T1 < T1.1 < T2 < T10 < T10.2 < T10.10 < T27 < T27.3 < T100.
func targetIDLess(a, b string) bool {
	return naturalLess(a, b)
}

// naturalLess is alphanumeric / natsort / version-style compare.
// Splits each string into alternating non-digit and digit runs; digit runs
// compare by integer magnitude (leading zeros ignored for value, then shorter
// raw run wins on ties). Residual: non-T prefixes still segment-split.
func naturalLess(a, b string) bool {
	ia, ib := 0, 0
	for ia < len(a) && ib < len(b) {
		aDig := a[ia] >= '0' && a[ia] <= '9'
		bDig := b[ib] >= '0' && b[ib] <= '9'
		if aDig && bDig {
			ea, eb := ia, ib
			for ea < len(a) && a[ea] >= '0' && a[ea] <= '9' {
				ea++
			}
			for eb < len(b) && b[eb] >= '0' && b[eb] <= '9' {
				eb++
			}
			if c := cmpDigitRun(a[ia:ea], b[ib:eb]); c != 0 {
				return c < 0
			}
			ia, ib = ea, eb
			continue
		}
		if aDig != bDig {
			// Digit vs non-digit at the same position: compare bytes.
			return a[ia] < b[ib]
		}
		// Both non-digit: consume equal run lengths one byte at a time.
		if a[ia] != b[ib] {
			return a[ia] < b[ib]
		}
		ia++
		ib++
	}
	return len(a)-ia < len(b)-ib
}

// cmpDigitRun compares digit strings as unsigned integers.
// Leading zeros do not change magnitude; on equal magnitude, shorter raw
// run is less (so "1" < "01"). Returns -1, 0, or 1.
func cmpDigitRun(a, b string) int {
	sa, sb := 0, 0
	for sa < len(a)-1 && a[sa] == '0' {
		sa++
	}
	for sb < len(b)-1 && b[sb] == '0' {
		sb++
	}
	da, db := a[sa:], b[sb:]
	if len(da) != len(db) {
		if len(da) < len(db) {
			return -1
		}
		return 1
	}
	if da < db {
		return -1
	}
	if da > db {
		return 1
	}
	// Equal magnitude: prefer fewer leading zeros (shorter raw).
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return 0
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

// GraphDiagramBlock is one Mermaid diagram in a multi-component pack (🎯T190).
// Each connected component (or the shared orphans block) is its own diagram;
// the panel packs blocks in a wrap grid instead of one mega LR strip.
type GraphDiagramBlock struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"` // "component" | "orphans"
	Title     string `json:"title,omitempty"`
	Mermaid   string `json:"mermaid"`
	NodeCount int    `json:"node_count"`
	EdgeCount int    `json:"edge_count"`
}

// GraphResponse is GET /api/frontier/graph JSON (🎯T185): entire unachieved
// dependency graph as Mermaid source for the Frontier Graph control.
// 🎯T190: Diagrams holds one Mermaid source per connected component (plus
// optional orphans block); Pack names the panel layout ("wrap-grid").
// Mermaid remains a multi-diagram joined source for pin/copy fallback.
type GraphResponse struct {
	Available bool                `json:"available"`
	Ledger    string              `json:"ledger,omitempty"`
	Cwd       string              `json:"cwd,omitempty"`
	Mermaid   string              `json:"mermaid,omitempty"`
	Diagrams  []GraphDiagramBlock `json:"diagrams,omitempty"`
	Pack      string              `json:"pack,omitempty"`
	NodeCount int                 `json:"node_count,omitempty"`
	EdgeCount int                 `json:"edge_count,omitempty"`
	Error     string              `json:"error,omitempty"`
	UpdatedAt string              `json:"updated_at,omitempty"`
}

// Frontier graph pack layout token (🎯T190). Hermetic tests assert this value.
const frontierGraphPackLayout = "wrap-grid"

// Diagram kind tokens (🎯T190).
const (
	graphDiagramKindComponent = "component"
	graphDiagramKindOrphans   = "orphans"
)

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

// Mermaid layout knobs for the unachieved Frontier Graph (🎯T190).
// Tight spacing + useMaxWidth keep ~30–50 nodes inside each component diagram;
// wrappingWidth softens wide labels. Multi-diagram wrap-grid packing lives
// in the panel CSS — Mermaid-native TD alone is rejected as the sole fix.
//
// mermaidMaxNodesPerDiagram (🎯T274): browser Mermaid/dagre hangs or never
// paints a single dense flowchart of 60+ nodes (orthograph external ledger
// was one 64-node component → empty viewer). Cap forces hierarchical re-split
// so each diagram stays renderable. Jevons-scale islands stay under the cap.
const (
	mermaidNodeSpacing         = 28
	mermaidRankSpacing         = 36
	mermaidWrappingWidth       = 180
	mermaidMaxNodesPerDiagram  = 24
)

// mermaidActiveGraphHeader is the init + direction prefix for one unachieved
// dependency diagram (🎯T185 + 🎯T190). Hermetic tests assert these tokens.
func mermaidActiveGraphHeader() string {
	return fmt.Sprintf(
		"%%%%{init: {'flowchart': {'useMaxWidth': true, 'nodeSpacing': %d, 'rankSpacing': %d, 'wrappingWidth': %d}}}%%%%\nflowchart TB\n",
		mermaidNodeSpacing, mermaidRankSpacing, mermaidWrappingWidth,
	)
}

// packIslandsFromAdj is the pure connected-component partitioner (🎯T190).
// activeIDs should already be sorted (stable island discovery order).
// adj is an undirected adjacency list (both directions present).
// Each island's ids are sorted by targetIDLess; islands ordered by first id.
func packIslandsFromAdj(activeIDs []string, adj map[string][]string) [][]string {
	if len(activeIDs) == 0 {
		return nil
	}
	visited := make(map[string]bool, len(activeIDs))
	var islands [][]string
	for _, start := range activeIDs {
		if visited[start] {
			continue
		}
		// BFS component.
		queue := []string{start}
		visited[start] = true
		var comp []string
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			comp = append(comp, cur)
			for _, nb := range adj[cur] {
				if visited[nb] {
					continue
				}
				visited[nb] = true
				queue = append(queue, nb)
			}
		}
		sort.Slice(comp, func(i, j int) bool {
			return targetIDLess(comp[i], comp[j])
		})
		islands = append(islands, comp)
	}
	sort.Slice(islands, func(i, j int) bool {
		if len(islands[i]) == 0 {
			return true
		}
		if len(islands[j]) == 0 {
			return false
		}
		return targetIDLess(islands[i][0], islands[j][0])
	})
	return islands
}

// splitOrphanComponents peels size-1 islands into a shared orphans list
// (🎯T190 option-1 special case of multi-diagram packing). Connected
// components (size ≥ 2) stay separate.
func splitOrphanComponents(islands [][]string) (connected [][]string, orphans []string) {
	for _, island := range islands {
		if len(island) == 0 {
			continue
		}
		if len(island) == 1 {
			orphans = append(orphans, island[0])
			continue
		}
		connected = append(connected, island)
	}
	sort.Slice(orphans, func(i, j int) bool {
		return targetIDLess(orphans[i], orphans[j])
	})
	return connected, orphans
}

// targetRootFamily maps hierarchical bullseye ids to their top-level root
// (T1.2.3 → T1, T10.1 → T10). Flat ids are their own family. Used when
// re-splitting oversized connected components for Mermaid (🎯T274).
func targetRootFamily(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return id
	}
	if i := strings.IndexByte(id, '.'); i >= 0 {
		return id[:i]
	}
	return id
}

// splitOversizedComponents re-partitions connected components that exceed
// maxNodes so each Mermaid diagram stays browser-renderable (🎯T274).
// Prefer hierarchical root-family groups packed into bins of ≤ maxNodes;
// hard-chunk when a single family still exceeds the cap.
// maxNodes ≤ 0 uses mermaidMaxNodesPerDiagram.
func splitOversizedComponents(connected [][]string, maxNodes int) [][]string {
	if maxNodes <= 0 {
		maxNodes = mermaidMaxNodesPerDiagram
	}
	var out [][]string
	for _, comp := range connected {
		if len(comp) == 0 {
			continue
		}
		if len(comp) <= maxNodes {
			out = append(out, comp)
			continue
		}
		// Group by root family (stable order by first-seen root, then sort).
		groups := make(map[string][]string)
		var roots []string
		seenRoot := make(map[string]bool)
		for _, id := range comp {
			root := targetRootFamily(id)
			if !seenRoot[root] {
				seenRoot[root] = true
				roots = append(roots, root)
			}
			groups[root] = append(groups[root], id)
		}
		sort.Slice(roots, func(i, j int) bool {
			return targetIDLess(roots[i], roots[j])
		})
		// Pack whole families into bins ≤ maxNodes (avoids one diagram per root
		// when families are tiny). Oversized families hard-chunk alone.
		var bin []string
		flush := func() {
			if len(bin) == 0 {
				return
			}
			out = append(out, bin)
			bin = nil
		}
		for _, root := range roots {
			ids := groups[root]
			sort.Slice(ids, func(i, j int) bool {
				return targetIDLess(ids[i], ids[j])
			})
			if len(ids) > maxNodes {
				flush()
				for i := 0; i < len(ids); i += maxNodes {
					end := i + maxNodes
					if end > len(ids) {
						end = len(ids)
					}
					out = append(out, append([]string(nil), ids[i:end]...))
				}
				continue
			}
			if len(bin)+len(ids) > maxNodes {
				flush()
			}
			bin = append(bin, ids...)
		}
		flush()
	}
	return out
}

// chunkIDList splits a sorted id list into maxNodes-sized slices (🎯T274).
func chunkIDList(ids []string, maxNodes int) [][]string {
	if maxNodes <= 0 {
		maxNodes = mermaidMaxNodesPerDiagram
	}
	if len(ids) == 0 {
		return nil
	}
	if len(ids) <= maxNodes {
		return [][]string{ids}
	}
	var out [][]string
	for i := 0; i < len(ids); i += maxNodes {
		end := i + maxNodes
		if end > len(ids) {
			end = len(ids)
		}
		out = append(out, append([]string(nil), ids[i:end]...))
	}
	return out
}

// countEdgesInSet returns how many directed raw edges have both ends in set.
func countEdgesInSet(edges []rawGraphEdge, set map[string]bool) int {
	n := 0
	for _, e := range edges {
		if set[e.from] && set[e.to] {
			n++
		}
	}
	return n
}

type rawGraphEdge struct{ from, to string }

// emitMermaidForNodes builds one flowchart TB diagram for the given nodes
// and among-set edges (🎯T190 multi-diagram model).
func emitMermaidForNodes(ids []string, nameOf map[string]string, edges []rawGraphEdge) (src string, edgeCount int) {
	var b strings.Builder
	b.WriteString(mermaidActiveGraphHeader())
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
		nid := mermaidSafeNodeID(id)
		label := nameOf[id]
		if label == "" {
			label = id
		} else {
			label = id + " · " + truncateMermaidLabel(label, 36)
		}
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", nid, escapeMermaidLabel(label))
	}
	for _, e := range edges {
		if !set[e.from] || !set[e.to] {
			continue
		}
		fmt.Fprintf(&b, "  %s -.->|needs| %s\n", mermaidSafeNodeID(e.from), mermaidSafeNodeID(e.to))
		edgeCount++
	}
	return b.String(), edgeCount
}

// packActiveGraphDiagrams turns connected components + optional orphans into
// ordered diagram blocks (largest/most-edges first, then orphans). Pure (🎯T190).
func packActiveGraphDiagrams(
	connected [][]string,
	orphans []string,
	nameOf map[string]string,
	edges []rawGraphEdge,
) []GraphDiagramBlock {
	type ranked struct {
		ids       []string
		nodes     int
		edgeCount int
		firstID   string
	}
	rankedList := make([]ranked, 0, len(connected))
	for _, comp := range connected {
		if len(comp) == 0 {
			continue
		}
		set := make(map[string]bool, len(comp))
		for _, id := range comp {
			set[id] = true
		}
		rankedList = append(rankedList, ranked{
			ids:       comp,
			nodes:     len(comp),
			edgeCount: countEdgesInSet(edges, set),
			firstID:   comp[0],
		})
	}
	// Largest / most-edges first (optional quasi-optimize for pack order).
	sort.SliceStable(rankedList, func(i, j int) bool {
		if rankedList[i].edgeCount != rankedList[j].edgeCount {
			return rankedList[i].edgeCount > rankedList[j].edgeCount
		}
		if rankedList[i].nodes != rankedList[j].nodes {
			return rankedList[i].nodes > rankedList[j].nodes
		}
		return targetIDLess(rankedList[i].firstID, rankedList[j].firstID)
	})

	blocks := make([]GraphDiagramBlock, 0, len(rankedList)+1)
	for i, r := range rankedList {
		src, ec := emitMermaidForNodes(r.ids, nameOf, edges)
		blocks = append(blocks, GraphDiagramBlock{
			ID:        fmt.Sprintf("c%d", i),
			Kind:      graphDiagramKindComponent,
			Title:     fmt.Sprintf("Component (%d nodes)", r.nodes),
			Mermaid:   src,
			NodeCount: r.nodes,
			EdgeCount: ec,
		})
	}
	// 🎯T274: cap orphan diagrams the same way — a 50-orphan strip also hangs.
	if len(orphans) > 0 {
		chunks := chunkIDList(orphans, mermaidMaxNodesPerDiagram)
		for i, chunk := range chunks {
			src, ec := emitMermaidForNodes(chunk, nameOf, edges)
			id := "orphans"
			title := fmt.Sprintf("Orphans (%d)", len(chunk))
			if len(chunks) > 1 {
				id = fmt.Sprintf("orphans_%d", i)
				title = fmt.Sprintf("Orphans part %d (%d)", i+1, len(chunk))
			}
			blocks = append(blocks, GraphDiagramBlock{
				ID:        id,
				Kind:      graphDiagramKindOrphans,
				Title:     title,
				Mermaid:   src,
				NodeCount: len(chunk),
				EdgeCount: ec,
			})
		}
	}
	return blocks
}

// joinGraphDiagramSources builds a pin/copy multi-diagram source with pack
// markers. Not valid as a single Mermaid render — panel uses diagrams[].
func joinGraphDiagramSources(pack string, blocks []GraphDiagramBlock) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%%%% jevons-frontier-pack pack=%s diagrams=%d %%%%\n", pack, len(blocks))
	for i, d := range blocks {
		fmt.Fprintf(&b, "%%%% --- diagram %d id=%s kind=%s --- %%%%\n", i, d.ID, d.Kind)
		b.WriteString(strings.TrimSpace(d.Mermaid))
		b.WriteByte('\n')
	}
	return b.String()
}

// activeGraphPack is the multi-diagram result of ledger graph build (🎯T190).
type activeGraphPack struct {
	Diagrams  []GraphDiagramBlock
	Pack      string
	Mermaid   string // joined multi-diagram pin source
	NodeCount int
	EdgeCount int
}

// emptyActiveGraphPack is a valid empty pack with one empty flowchart block.
func emptyActiveGraphPack() activeGraphPack {
	emptySrc := mermaidActiveGraphHeader()
	blocks := []GraphDiagramBlock{{
		ID:      "empty",
		Kind:    graphDiagramKindComponent,
		Title:   "Empty",
		Mermaid: emptySrc,
	}}
	return activeGraphPack{
		Diagrams: blocks,
		Pack:     frontierGraphPackLayout,
		Mermaid:  joinGraphDiagramSources(frontierGraphPackLayout, blocks),
	}
}

// computeActiveGraphPackFromLedger builds multi-diagram Mermaid for all
// unachieved (active) targets and depends_on edges among them (🎯T185/T190).
// Achieved / set_aside nodes are omitted; edges to non-active deps are dropped.
// 🎯T190: partition into connected components; each component is its own
// Mermaid diagram; size-1 isolates share one orphans diagram; pack=wrap-grid.
func computeActiveGraphPackFromLedger(ledgerPath string) (activeGraphPack, error) {
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		return activeGraphPack{}, err
	}
	var doc bullseyeLedger
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return activeGraphPack{}, fmt.Errorf("parse ledger: %w", err)
	}
	if doc.Targets == nil || len(doc.Targets) == 0 {
		return emptyActiveGraphPack(), nil
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

	rawEdges := make([]rawGraphEdge, 0)
	seenEdge := make(map[string]bool)
	adj := make(map[string][]string, len(activeIDs))
	for _, id := range activeIDs {
		adj[id] = nil
	}
	for _, id := range activeIDs {
		t := doc.Targets[id]
		for _, dep := range t.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" || !active[dep] {
				continue
			}
			key := id + "\x00" + dep
			if seenEdge[key] {
				continue
			}
			seenEdge[key] = true
			rawEdges = append(rawEdges, rawGraphEdge{from: id, to: dep})
			adj[id] = append(adj[id], dep)
			adj[dep] = append(adj[dep], id)
		}
	}
	sort.Slice(rawEdges, func(i, j int) bool {
		if rawEdges[i].from != rawEdges[j].from {
			return targetIDLess(rawEdges[i].from, rawEdges[j].from)
		}
		return targetIDLess(rawEdges[i].to, rawEdges[j].to)
	})

	islands := packIslandsFromAdj(activeIDs, adj)
	connected, orphans := splitOrphanComponents(islands)
	// 🎯T274: one mega connected component (e.g. orthograph 64-node ledger)
	// is not browser-renderable as a single Mermaid diagram — re-split.
	connected = splitOversizedComponents(connected, mermaidMaxNodesPerDiagram)
	blocks := packActiveGraphDiagrams(connected, orphans, nameOf, rawEdges)

	// Empty active set after filter: still return pack shell.
	if len(blocks) == 0 {
		emptySrc := mermaidActiveGraphHeader()
		blocks = []GraphDiagramBlock{{
			ID:      "empty",
			Kind:    graphDiagramKindComponent,
			Title:   "Empty",
			Mermaid: emptySrc,
		}}
	}

	return activeGraphPack{
		Diagrams:  blocks,
		Pack:      frontierGraphPackLayout,
		Mermaid:   joinGraphDiagramSources(frontierGraphPackLayout, blocks),
		NodeCount: len(activeIDs),
		EdgeCount: len(rawEdges),
	}, nil
}

// computeActiveGraphMermaidFromLedger is the legacy single-string entry
// (tests + callers that only need joined source). Prefer pack for product.
func computeActiveGraphMermaidFromLedger(ledgerPath string) (mermaid string, nodeCount, edgeCount int, err error) {
	p, err := computeActiveGraphPackFromLedger(ledgerPath)
	if err != nil {
		return "", 0, 0, err
	}
	return p.Mermaid, p.NodeCount, p.EdgeCount, nil
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

	pack, err := computeActiveGraphPackFromLedger(ledger)
	if err != nil {
		// Fallback: bullseye CLI graph export (scope=active ≈ unachieved).
		out, cliErr := runBullseyeCLI("query", "--view", "graph", "--scope", "active", "--cwd", abs)
		if cliErr == nil && strings.TrimSpace(out) != "" {
			// Strip optional ```mermaid fences from CLI output.
			src := stripMermaidFenceCLI(out)
			if src != "" {
				resp.Mermaid = src
				resp.Available = true
				// Single CLI blob → one component diagram for multi-path UI.
				resp.Pack = frontierGraphPackLayout
				resp.Diagrams = []GraphDiagramBlock{{
					ID:      "cli",
					Kind:    graphDiagramKindComponent,
					Title:   "Active graph",
					Mermaid: src,
				}}
				return resp
			}
		}
		resp.Error = fmt.Sprintf("read ledger: %v", err)
		return resp
	}
	resp.Mermaid = pack.Mermaid
	resp.Diagrams = pack.Diagrams
	resp.Pack = pack.Pack
	resp.NodeCount = pack.NodeCount
	resp.EdgeCount = pack.EdgeCount
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
	// 🎯T195: seed achieved set before watching so historical achieves do not
	// mass-reap engaged agents on first ledger event after bind.
	s.seedAchievedFromLedger(ledger)
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
		// 🎯T195: reap implementers engaged on newly achieved targets before
		// broadcasting the frontier refresh (ledger is the achieve signal).
		s.maybeReapOnLedgerAchieve(path)
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
