// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/pochildwake"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/staffops"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// AgentStatusPOUnactedChildReports is the typed fault on agent_list and
// GET /api/agents when 🎯T1193 fires.
const AgentStatusPOUnactedChildReports = "po_unacted_child_reports"

const poChildWakeSymptomPrefix = "po_unacted_child_reports:"

func poChildWakeSymptom(poName string) string {
	return poChildWakeSymptomPrefix + strings.TrimSpace(poName)
}

type poChildWakeFaultSnap struct {
	PO      string
	Reports []pochildwake.Report
	Detail  string
	At      time.Time
}

type poChildWakeFaultCache struct {
	mu    sync.RWMutex
	byPO  map[string]poChildWakeFaultSnap
	clear map[string]struct{}
}

func (c *poChildWakeFaultCache) set(snap poChildWakeFaultSnap) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.byPO == nil {
		c.byPO = make(map[string]poChildWakeFaultSnap)
	}
	c.byPO[snap.PO] = snap
	delete(c.clear, snap.PO)
	c.mu.Unlock()
}

func (c *poChildWakeFaultCache) clearPO(name string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.byPO != nil {
		delete(c.byPO, name)
	}
	if c.clear == nil {
		c.clear = make(map[string]struct{})
	}
	c.clear[name] = struct{}{}
	c.mu.Unlock()
}

func (c *poChildWakeFaultCache) line(poName string) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.RLock()
	snap, ok := c.byPO[poName]
	c.mu.RUnlock()
	if !ok || len(snap.Reports) == 0 {
		return "", false
	}
	return fmt.Sprintf("po_unacted_child_reports: %s", pochildwake.FormatWakeBody(snap.Reports)), true
}

func (s *Server) ensurePOChildWakeFaultCache() *poChildWakeFaultCache {
	if s == nil {
		return nil
	}
	if s.poChildWakeFaults == nil {
		s.poChildWakeFaults = &poChildWakeFaultCache{}
	}
	return s.poChildWakeFaults
}

// POUnactedChildReportsFaultLine is the /api/agents and agent_list annotation.
func (s *Server) POUnactedChildReportsFaultLine(poName string) (string, bool) {
	c := s.ensurePOChildWakeFaultCache()
	if c == nil {
		return "", false
	}
	return c.line(poName)
}

func (s *Server) notePOTurnEndedForChildWake(name string) {
	if s == nil || strings.TrimSpace(name) == "" || s.registry == nil {
		return
	}
	def := s.registry.Def(name)
	if def == nil || !purposeIsProductOwner(s.seatRoleOf(*def)) {
		return
	}
	now := s.deliveryNow().UTC()
	s.mu.Lock()
	if s.poTurnEndedAt == nil {
		s.poTurnEndedAt = map[string]time.Time{}
	}
	s.poTurnEndedAt[name] = now
	s.mu.Unlock()
}

func (s *Server) poLastTurnEnded(name string) time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.Lock()
	t := s.poTurnEndedAt[name]
	s.mu.Unlock()
	return t
}

func storedTerminalReportKind(text string) (string, bool) {
	if !IsStoredTerminalReportKind(text) {
		return "", false
	}
	m, _ := envelope.Parse(text)
	if m == nil {
		return "", false
	}
	return string(m.Kind), true
}

type childSeat struct {
	agent  string
	target string
}

func (s *Server) childSeatsForPO(poName, workdir string) []childSeat {
	if s == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []childSeat
	add := func(agent, target string) {
		agent = strings.TrimSpace(agent)
		if agent == "" {
			return
		}
		if _, ok := seen[agent]; ok {
			return
		}
		seen[agent] = struct{}{}
		out = append(out, childSeat{agent: agent, target: strings.TrimSpace(target)})
	}
	if s.registry != nil {
		for _, d := range s.registry.List() {
			if strings.TrimSpace(d.Parent) != poName {
				continue
			}
			purpose := strings.TrimSpace(d.Purpose)
			if purpose == "" {
				purpose = claudia.PurposeWork
			}
			if purpose != claudia.PurposeWork {
				continue
			}
			add(d.Name, d.TargetID)
		}
	}
	stateDir := s.agentReportStateDir()
	if stateDir != "" {
		entries, err := os.ReadDir(filepath.Join(stateDir, agentreport.DirName))
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				agent := e.Name()
				parent, target := s.childParentTarget(agent, poName, workdir)
				if parent != poName {
					continue
				}
				add(agent, target)
			}
		}
	}
	return out
}

func (s *Server) childParentTarget(agent, poName, workdir string) (parent, target string) {
	if s.registry != nil {
		if def := s.registry.Def(agent); def != nil {
			if strings.TrimSpace(def.Parent) == poName {
				return poName, strings.TrimSpace(def.TargetID)
			}
		}
	}
	return s.reapedChildLineage(agent, workdir)
}

func (s *Server) reapedChildLineage(agent, workdir string) (parent, target string) {
	if n, ok := s.RemovalAccount().NoticeFor(agent); ok {
		return strings.TrimSpace(n.Parent), strings.TrimSpace(n.TargetID)
	}
	if s.reapVerify != nil {
		for _, p := range s.reapVerify.List() {
			if strings.TrimSpace(p.Seat) != agent {
				continue
			}
			if workdir != "" && p.Repo != "" && !targetfile.SameLedger(targetfile.LedgerKey(workdir), targetfile.LedgerKey(p.Repo)) {
				continue
			}
			return strings.TrimSpace(p.Owes), strings.TrimSpace(p.TargetID)
		}
	}
	return "", ""
}

func (s *Server) collectChildTerminalReports(poName, workdir string) []pochildwake.Report {
	stateDir := s.agentReportStateDir()
	if stateDir == "" {
		return nil
	}
	var out []pochildwake.Report
	for _, seat := range s.childSeatsForPO(poName, workdir) {
		rec, err := agentreport.LatestDeclared(stateDir, seat.agent)
		if err != nil || strings.TrimSpace(rec.Text) == "" {
			continue
		}
		kind, ok := storedTerminalReportKind(rec.Text)
		if !ok {
			continue
		}
		target := seat.target
		if target == "" {
			if m, _ := envelope.Parse(rec.Text); m != nil {
				target = strings.TrimSpace(m.Target)
			}
		}
		out = append(out, pochildwake.Report{
			Agent: seat.agent, Target: target, Kind: kind,
			ReportID: rec.ID, StoredAt: rec.At,
		})
	}
	return out
}

func trackPOChildWakeGrace(rt *sentinelRuntime, poName, phase string, alive bool, now time.Time, grace time.Duration) bool {
	sym := poChildWakeSymptom(poName)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if !alive || !poFanoutPhaseIdle(phase) {
		delete(rt.firstSeen, sym)
		return false
	}
	first, seen := rt.firstSeen[sym]
	if !seen {
		rt.firstSeen[sym] = now
		first = now
	}
	return now.Sub(first) >= grace
}

func staffopsUnactedReports(reports []pochildwake.Report) []staffops.POUnactedChildReportItemObs {
	if len(reports) == 0 {
		return nil
	}
	out := make([]staffops.POUnactedChildReportItemObs, 0, len(reports))
	for _, r := range reports {
		out = append(out, staffops.POUnactedChildReportItemObs{
			Agent: r.Agent, Target: r.Target, Kind: r.Kind, ReportID: r.ReportID,
		})
	}
	return out
}

// samplePOUnactedChildReports classifies idle POs with unacted child terminal
// reports (🎯T1193) for staffops and agent_list readers.
func (s *Server) samplePOUnactedChildReports(rt *sentinelRuntime, overseer, workdir string, now time.Time, grace time.Duration, running func(string) bool) []staffops.POUnactedChildReportObs {
	if s == nil || s.registry == nil || rt == nil {
		return nil
	}
	if running == nil {
		running = func(name string) bool {
			proc := s.registry.Get(name)
			return proc != nil && s.seatState(name).Alive == seatstate.Yes
		}
	}
	wantLedger := targetfile.LedgerKey(workdir)
	var out []staffops.POUnactedChildReportObs
	for _, d := range s.registry.List() {
		poName := strings.TrimSpace(d.Name)
		if poName == "" || poName == overseer || !purposeIsProductOwner(s.seatRoleOf(d)) {
			continue
		}
		if workdir != "" && !targetfile.SameLedger(wantLedger, targetfile.LedgerKey(d.WorkDir)) {
			continue
		}
		alive := running(poName)
		phase := s.activityPhaseForStall(poName)
		graceElapsed := trackPOChildWakeGrace(rt, poName, phase, alive, now, grace)
		poObs := pochildwake.POObs{
			Name: poName, Alive: alive, Phase: phase, GraceElapsed: graceElapsed,
			LastTurnEnded: s.poLastTurnEnded(poName),
		}
		all := s.collectChildTerminalReports(poName, workdir)
		pending := pochildwake.Unacted(poObs, all, func(id string) bool {
			return s.parentReportAlreadyOffered(poName, id)
		})
		verdict, detail := pochildwake.Classify(poObs, pending)
		if pochildwake.IsFault(verdict) {
			obs := staffops.POUnactedChildReportObs{
				PO: poName, Reports: staffopsUnactedReports(pending), Detail: detail,
			}
			out = append(out, obs)
			s.ensurePOChildWakeFaultCache().set(poChildWakeFaultSnap{
				PO: poName, Reports: pending, Detail: detail, At: now,
			})
			s.logLifecycle(compSentinel, "po_unacted_child_reports", "error", map[string]any{
				"po": poName, "reports": len(pending), "detail": detail,
			})
		} else if c := s.ensurePOChildWakeFaultCache(); c != nil {
			c.clearPO(poName)
		}
	}
	rt.mu.Lock()
	rt.poUnactedChildWake = out
	rt.mu.Unlock()
	return out
}

// tickPOUnactedChildReportWake delivers one wake per PO with a typed list of
// unacted child terminal reports (🎯T1193).
func (s *Server) tickPOUnactedChildReportWake(rt *sentinelRuntime, overseer, workdir string, now time.Time, grace time.Duration, dryRun bool, running func(string) bool) {
	if s == nil || rt == nil || dryRun {
		return
	}
	rt.mu.Lock()
	faults := rt.poUnactedChildWake
	rt.mu.Unlock()
	for _, f := range faults {
		poName := strings.TrimSpace(f.PO)
		if poName == "" || len(f.Reports) == 0 {
			continue
		}
		pending := make([]pochildwake.Report, 0, len(f.Reports))
		for _, r := range f.Reports {
			pending = append(pending, pochildwake.Report{
				Agent: r.Agent, Target: r.Target, Kind: r.Kind, ReportID: r.ReportID,
			})
		}
		body := pochildwake.FormatWakeBody(pending)
		if body == "" {
			continue
		}
		msg := "[daemon wake 🎯T1193] Unacted child terminal reports — integrate, achieve, or route:\n" + body
		if err := s.deliverSentinel(poName, staffops.SentinelEventSource, msg); err != nil {
			s.logLifecycle(compSentinel, "po_unacted_child_reports", "error", map[string]any{
				"po": poName, "err": err.Error(),
			})
			continue
		}
		for _, r := range pending {
			s.noteParentReportOffered(poName, r.ReportID)
		}
	}
}
