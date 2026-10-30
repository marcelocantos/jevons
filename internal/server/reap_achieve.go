// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/marcelocantos/claudia"
	"gopkg.in/yaml.v3"

	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/roles"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// 🎯T195: after a mission target is achieved on the bullseye ledger, reap
// work agents engaged on that target (TargetID equality) even when their
// terminal report was imperfect or missing. Complements T165 report-path
// reaping. POs / overseer / asides stay; deliberate stop without kill is
// unchanged. Multi-target agents without a matching TargetID stay.

// durableFleetAgentForAchieve mirrors mcpserver.DurableFleetAgent so this
// package does not import mcpserver (cycle). Keep in sync. The seat's
// declared role decides (AgentDef.Role, else the role its purpose implies);
// isDurable answers for what the row alone cannot name — the overseer and,
// on the production path, a seat whose recorded role (agent_roles.json) is
// durable. The name does not decide.
func durableFleetAgentForAchieve(name string, def claudia.AgentDef, isDurable func(string) bool) bool {
	if name == "" {
		return true
	}
	if isDurable != nil && isDurable(name) {
		return true
	}
	return durableRole(roles.OfSeat(def.Role, def.Purpose))
}

// durableRole is a role that auto-achieve reaping never removes.
func durableRole(role string) bool {
	switch roles.Normalize(role) {
	case roles.Overseer, roles.Aside, roles.ProductOwner:
		return true
	default:
		return false
	}
}

// ReapWorkAgentsOnTargetAchieve removes non-durable work agents whose
// TargetID matches the achieved target (and their descendant subtrees).
// Returns names removed. No-ops when none engaged or only durable roles match.
//
// 🎯T389: scopeWorkdir is a path inside the repo whose ledger recorded the
// achieve. This is the reaping direction of the collision, and the damaging
// one — without it, achieving 🎯T19 in claudia kills orthograph's 🎯T19 worker
// mid-flight.
//
// 🎯T435: acct accounts for each removal in the event log. This is the path
// that ran twice on 2026-08-10 while saying nothing the overseer could read,
// and a correct reap that looks like an orphaning costs more than it saves.
// A nil acct still reaps (slog-only) so unwired callers are not blocked.
func ReapWorkAgentsOnTargetAchieve(reg *claudia.Registry, acct *fleetlog.Account, targetID, scopeWorkdir string, isOverseer func(string) bool, ownerMessageGuard ...func(string) bool) ([]string, error) {
	if reg == nil {
		return nil, fmt.Errorf("agent registry not available")
	}
	want := NormalizeTargetID(targetID)
	if want == "" {
		return nil, fmt.Errorf("target_id is required")
	}
	engaged := AgentsEngagedOnTarget(reg, want, scopeWorkdir)
	if len(engaged) == 0 {
		return nil, nil
	}
	// Filter durable roles (PO/overseer/aside). Owner engagement stop may
	// still kill more broadly via stopEngagement; auto-achieve reaping is
	// implementer hygiene only.
	var toReap []string
	for _, name := range engaged {
		def := reg.Def(name)
		if def == nil {
			continue
		}
		if durableFleetAgentForAchieve(name, *def, isOverseer) || hasRegisteredDescendants(reg, name) || (len(ownerMessageGuard) > 0 && ownerMessageGuard[0] != nil && ownerMessageGuard[0](name)) {
			continue
		}
		toReap = append(toReap, name)
	}
	if len(toReap) == 0 {
		return nil, nil
	}
	sort.Strings(toReap)
	rm := fleetlog.Removal{
		Reason: fleetlog.ReasonReapAchieve,
		Detail: fmt.Sprintf("reaped on achieve of 🎯%s", want),
		Fields: map[string]any{"achieved_target_id": want},
	}
	var removed []string
	for _, name := range toReap {
		if reg.Def(name) == nil {
			continue
		}
		ok, err := acct.Remove(reg, name, rm)
		if ok {
			removed = append(removed, name)
		}
		if err != nil {
			return removed, fmt.Errorf("reap achieve %q: %w", name, err)
		}
	}
	return removed, nil
}

// hasRegisteredDescendants keeps parents of still-engaged seats parked.
func hasRegisteredDescendants(reg *claudia.Registry, name string) bool {
	parents := make(map[string]string)
	for _, d := range reg.List() {
		parents[d.Name] = d.Parent
	}
	for child := range parents {
		seen := map[string]bool{}
		for parent := parents[child]; parent != "" && !seen[parent]; parent = parents[parent] {
			if parent == name {
				return true
			}
			seen[parent] = true
		}
	}
	return false
}

// SetClosedTargetOwnerMessageGuard keeps owner-origin obligations deliverable
// through the same guard used by finish-report auto-reap.
func (s *Server) SetClosedTargetOwnerMessageGuard(held func(string) bool) {
	s.closedTargetOwnerMessages = held
}

// sweepStoppedClosedTargets removes historical parked residue on boot and
// newly parked residue on ledger changes without interrupting live workers.
func (s *Server) sweepStoppedClosedTargets(ledgerPath string, closed map[string]bool, parked func(string) bool) {
	if s == nil || s.registry == nil || len(closed) == 0 {
		return
	}
	recorded, err := roles.OpenAssignments(roles.DefaultAssignmentsPath(s.stateDir))
	if err != nil {
		slog.Warn("T1340 parked-seat sweep skipped: role assignments unreadable", "err", err)
		return
	}
	overseer := s.overseerName
	if overseer == "" {
		overseer = defaultOverseerName
	}
	reg := s.registry
	scope := targetfile.LedgerKey(filepath.Dir(ledgerPath))
	var removed []string
	for _, def := range reg.List() {
		tid := NormalizeTargetID(def.TargetID)
		if (s.closedTargetOwnerMessages != nil && s.closedTargetOwnerMessages(def.Name)) || !closed[tid] || !targetfile.SameLedger(scope, targetfile.LedgerKey(def.WorkDir)) || (parked != nil && !parked(def.Name)) || reg.Get(def.Name) != nil || durableFleetAgentForAchieve(def.Name, def, func(n string) bool { return n == overseer || durableRole(recorded.Get(n)) }) || hasRegisteredDescendants(reg, def.Name) {
			continue
		}
		rm := fleetlog.Removal{Reason: fleetlog.ReasonReapAchieve, Detail: fmt.Sprintf("reaped parked seat on closed target 🎯%s", tid), Fields: map[string]any{"achieved_target_id": tid}}
		if ok, err := s.RemovalAccount().Remove(reg, def.Name, rm); err != nil {
			slog.Warn("T1340 parked-seat reap failed", "agent", def.Name, "err", err)
		} else if ok {
			removed = append(removed, def.Name)
		}
	}
	if len(removed) > 0 {
		slog.Info("T1340 reaped parked seats on closed targets", "removed", removed)
		s.NotifyAgentsChanged()
	}
}

const (
	ledgerStatusAchieved = "achieved"
	ledgerStatusSetAside = "set_aside"
)

// listAchievedTargetIDs reads a bullseye ledger and returns the closed target
// IDs (normalized): achieved, or set aside. A set-aside target will not be
// worked, so its worker has nothing left either (🎯T985). Excluding it left
// jv-t1138-auto registered after T1138 was set aside; when its process later
// died the cockpit flagged it "should be running; seat is down" (2026-10-23).
func listAchievedTargetIDs(ledgerPath string) (map[string]bool, error) {
	out := make(map[string]bool)
	if ledgerPath == "" {
		return out, nil
	}
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		return nil, err
	}
	var doc bullseyeLedger
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse ledger: %w", err)
	}
	for id, t := range doc.Targets {
		if st := strings.ToLower(strings.TrimSpace(t.Status)); st == ledgerStatusAchieved || st == ledgerStatusSetAside {
			nid := NormalizeTargetID(id)
			if nid != "" {
				out[nid] = true
			}
		}
	}
	return out, nil
}

// newlyAchievedTargetIDs returns ids present in curr but not prev.
func newlyAchievedTargetIDs(prev, curr map[string]bool) []string {
	if len(curr) == 0 {
		return nil
	}
	var out []string
	for id := range curr {
		if prev == nil || !prev[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// SweepParkedClosedTargetsAtStartup discovers every ledger represented by a
// registered mission. It runs before reattach so stale parked seats never
// enter the delivery/revival loop, even when no frontier client has polled.
func (s *Server) SweepParkedClosedTargetsAtStartup(parked func(string) bool) {
	if s == nil || s.registry == nil {
		return
	}
	seen := map[string]bool{}
	for _, d := range s.registry.List() {
		if d.TargetID == "" {
			continue
		}
		ledger := targetfile.LedgerKey(d.WorkDir)
		if ledger == "" || seen[ledger] {
			continue
		}
		seen[ledger] = true
		closed, err := listAchievedTargetIDs(ledger)
		if err != nil {
			slog.Warn("T1340 startup ledger unreadable", "ledger", ledger, "err", err)
			continue
		}
		s.sweepStoppedClosedTargets(ledger, closed, parked)
	}
}

// seedAchievedFromLedger records the current closed set and reaps only
// stopped historical residue (never active workers) at the first watch bind.
func (s *Server) seedAchievedFromLedger(ledgerPath string) {
	if s == nil || ledgerPath == "" {
		return
	}
	ledgerPath = filepath.Clean(ledgerPath)
	curr, err := listAchievedTargetIDs(ledgerPath)
	if err != nil {
		slog.Debug("T195 seed achieved set failed", "ledger", ledgerPath, "err", err)
		return
	}
	s.mu.Lock()
	if s.achievedTargetsSeen == nil {
		s.achievedTargetsSeen = make(map[string]map[string]bool)
	}
	s.achievedTargetsSeen[ledgerPath] = curr
	s.mu.Unlock()
	s.sweepStoppedClosedTargets(ledgerPath, curr, nil)
}

// maybeReapOnLedgerAchieve diffs the ledger achieved set against the last
// seen set and reaps work agents engaged on newly achieved targets (🎯T195).
func (s *Server) maybeReapOnLedgerAchieve(ledgerPath string) {
	if s == nil || ledgerPath == "" {
		return
	}
	ledgerPath = filepath.Clean(ledgerPath)
	curr, err := listAchievedTargetIDs(ledgerPath)
	if err != nil {
		slog.Debug("T195 list achieved failed", "ledger", ledgerPath, "err", err)
		return
	}

	s.mu.Lock()
	if s.achievedTargetsSeen == nil {
		s.achievedTargetsSeen = make(map[string]map[string]bool)
	}
	prev := s.achievedTargetsSeen[ledgerPath]
	if prev == nil {
		// First observation: seed only (no reap on cold start).
		s.achievedTargetsSeen[ledgerPath] = curr
		s.mu.Unlock()
		return
	}
	newly := newlyAchievedTargetIDs(prev, curr)
	s.achievedTargetsSeen[ledgerPath] = curr
	reg := s.registry
	overseer := s.overseerName
	if overseer == "" {
		overseer = defaultOverseerName
	}
	stateDir := s.stateDir
	s.mu.Unlock()

	s.sweepStoppedClosedTargets(ledgerPath, curr, nil)
	if reg == nil || len(newly) == 0 {
		return
	}
	// A product owner is named by the role recorded at spawn
	// (agent_roles.json, written by the mcpserver half), not by its purpose
	// (always work on a live PO) or its name (🎯T766.4). A store that cannot
	// be read reaps nothing: a PO wrongly reaped costs more than a worker
	// left for the next pass.
	recorded, err := roles.OpenAssignments(roles.DefaultAssignmentsPath(stateDir))
	if err != nil {
		slog.Warn("T195 reap on achieve skipped: role assignments unreadable", "err", err)
		return
	}
	isO := func(n string) bool { return n == overseer || durableRole(recorded.Get(n)) }
	// 🎯T389: the achieve belongs to this ledger, and so does every id in it.
	// The ledger's own directory is the scope — never the daemon's cwd, which
	// is some other repo whenever the watch fires for a second ledger.
	scope := filepath.Dir(ledgerPath)
	acct := s.RemovalAccount()
	var anyRemoved bool
	for _, tid := range newly {
		removed, err := ReapWorkAgentsOnTargetAchieve(reg, acct, tid, scope, isO, s.closedTargetOwnerMessages)
		if err != nil {
			slog.Warn("T195 reap on achieve failed", "target_id", tid, "err", err)
			continue
		}
		if len(removed) > 0 {
			anyRemoved = true
			slog.Info("auto-reaped work agents after target achieve",
				"target_id", tid, "removed", removed)
		}
	}
	if anyRemoved {
		s.NotifyAgentsChanged()
	}
}
