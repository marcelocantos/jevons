// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/poproactive"
	"github.com/marcelocantos/jevons/internal/staffops"
	"github.com/marcelocantos/jevons/internal/targetfile"
	"github.com/marcelocantos/jevons/internal/turnev"
)

// 🎯T219: durable background sentinel — observe→classify→act while jevonsd is up.
// Pure watcher: control-plane repairs only; residual → file+PO mission; no Ship;
// no product code implement. Complements T204/T207/T85 mechanical floor and
// T325.4 one-shot staff ops cycle.

const (
	// DefaultSentinelInterval is how often the continuous loop ticks.
	DefaultSentinelInterval = 2 * time.Minute
	// DefaultSentinelEventWindow bounds eventlog anomaly clustering.
	DefaultSentinelEventWindow = 15 * time.Minute
	// DefaultSentinelMechanicalGrace: escalate mechanical residual after this.
	DefaultSentinelMechanicalGrace = staffops.DefaultMechanicalGrace
	// compSentinel is the lifecycle / eventlog component for audit trail.
	compSentinel = "sentinel"
)

// SentinelLoopArgs configures StartSentinelLoop.
type SentinelLoopArgs struct {
	Server      *Server
	StateDir    string
	Workdir     string // bullseye ledger cwd for frontier depth (optional)
	Overseer    string // default jevons
	DefaultPO   string // default jevons-po
	Interval    time.Duration
	EventWindow time.Duration
	// MaxActionsPerHour overrides staffops.DefaultMaxActionsPerHour when >0.
	MaxActionsPerHour int
	// DryRun never delivers or records budget/cooldown (tests / observe-only).
	DryRun bool
	// AutoSpawnPaused is config frontier_consume.disabled (🎯T407). When
	// true, sample treats the fleet as unable to run and will not emit
	// stall:frontier / po_stall file+PO. Combined with Server.AutoSpawnPaused.
	AutoSpawnPaused bool
	// Now optional clock.
	Now func() time.Time
	// ProcessRunning optionally overrides liveness for the PO fan-out read
	// (🎯T380), mirroring IdleNudgeSweepArgs.ProcessRunning: hermetic fixtures
	// register agent defs without OS processes behind them. Nil → registry
	// Get(name).Alive().
	ProcessRunning func(name string) bool
	// OnResult optional hook after each cycle (tests).
	OnResult func(staffops.CycleResult, SentinelActResult)
}

// SentinelActResult is what the harness did after pure classification.
type SentinelActResult struct {
	Repaired  bool
	FiledToPO bool
	Delivered bool
	Skipped   string // why no act (empty when acted or healthy)
	AuditNote string
}

// sentinelRuntime holds durable-in-process cooldown/budget for the loop + MCP.
type sentinelRuntime struct {
	mu       sync.Mutex
	cooldown staffops.Cooldown
	budget   staffops.ActionBudget
	// firstSeen maps mechanical symptom → first observation time (grace).
	firstSeen map[string]time.Time
	// poFanout maps product-owner name → cross-cycle fan-out bookkeeping
	// (🎯T380): whether a turn completed since the last cycle, and the child
	// count it began with. One sample cannot see a turn; two can.
	poFanout map[string]poFanoutState
	// lastPrimary / lastTick for status tool.
	lastPrimary staffops.Action
	lastTick    time.Time
	cycles      int
}

func newSentinelRuntime(maxPerHour int) *sentinelRuntime {
	if maxPerHour <= 0 {
		maxPerHour = staffops.DefaultMaxActionsPerHour
	}
	return &sentinelRuntime{
		cooldown: staffops.Cooldown{
			LastFile: make(map[string]time.Time),
			Duration: staffops.DefaultCooldown,
		},
		budget:    staffops.ActionBudget{MaxPerHour: maxPerHour},
		firstSeen: make(map[string]time.Time),
		poFanout:  make(map[string]poFanoutState),
	}
}

// ensureSentinelRuntime returns the shared runtime (staffOps-adjacent).
func (s *Server) ensureSentinelRuntime(maxPerHour int) *sentinelRuntime {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sentinel == nil {
		s.sentinel = newSentinelRuntime(maxPerHour)
	}
	// Also keep staffOps cooldown for one-shot T325.4 sharing symptom suppress.
	if s.staffOps == nil {
		s.staffOps = &staffOpsState{
			cooldown: staffops.Cooldown{
				LastFile: make(map[string]time.Time),
				Duration: staffops.DefaultCooldown,
			},
		}
	}
	return s.sentinel
}

// SetAutoSpawnPaused records config frontier_consume.disabled for 🎯T407.
// The sentinel reads this each sample; a paused fleet is a block, not a stall.
func (s *Server) SetAutoSpawnPaused(paused bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.autoSpawnPaused = paused
	s.mu.Unlock()
}

// AutoSpawnPaused reports whether auto-spawn is deliberately paused.
func (s *Server) AutoSpawnPaused() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autoSpawnPaused
}

// registerSentinelTools exposes status + one-shot dry/live tick for 🎯T219.
func (s *Server) registerSentinelTools() {
	s.ensureSentinelRuntime(0)
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_sentinel_cycle",
			mcp.WithDescription("Run one durable-sentinel observe→classify→act cycle (🎯T219). Samples overseer/fleet/eventlog/frontier product surfaces, pure-classifies harness-ok|repair|file+PO|ignore (grace, cooldown, max actions/hour), then control-plane repair and/or mission to jevons-po. Continuous loop runs while jevonsd is up; this tool is the same policy for dry_run/status. No product implement; no Ship."),
			mcp.WithBoolean("dry_run", mcp.Description("If true, classify only — no repair, no deliver, no budget/cooldown update.")),
			mcp.WithBoolean("act", mcp.Description("If true (default when dry_run=false), perform repair/file+PO act. When false with dry_run=false, still updates cooldown only if act would file.")),
		),
		s.handleSentinelCycle,
	)
}

func (s *Server) handleSentinelCycle(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	dryRun := false
	if v, ok := args["dry_run"].(bool); ok {
		dryRun = v
	}
	// act defaults true when not dry_run; dry_run always suppresses act + budget/cooldown.
	act := !dryRun
	if v, ok := args["act"].(bool); ok {
		act = v && !dryRun
	}
	res, actRes := s.runSentinelCycle(SentinelLoopArgs{
		Server:   s,
		DryRun:   !act,
		StateDir: s.stateDir,
		Workdir:  s.workerWD,
	})

	rt := s.ensureSentinelRuntime(0)
	rt.mu.Lock()
	cycles := rt.cycles
	last := rt.lastPrimary
	tick := rt.lastTick
	rt.mu.Unlock()

	tickStr := ""
	if !tick.IsZero() {
		tickStr = tick.UTC().Format(time.RFC3339)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Sentinel cycle (🎯T219): primary=%s signals=%d filed=%d repair=%d dry_run=%v repaired=%v filed_po=%v delivered=%v cycles=%d last_primary=%s last_tick=%s\n",
		res.Primary, len(res.Decisions), len(res.FiledSymptoms), len(res.RepairSymptoms),
		!act, actRes.Repaired, actRes.FiledToPO, actRes.Delivered, cycles, last, tickStr)
	if actRes.Skipped != "" {
		fmt.Fprintf(&b, "skipped=%s\n", actRes.Skipped)
	}
	if actRes.AuditNote != "" {
		fmt.Fprintf(&b, "audit=%s\n", actRes.AuditNote)
	}
	b.WriteByte('\n')
	b.WriteString(res.WireText)
	return mcp.NewToolResultText(b.String()), nil
}

// StartSentinelLoop runs continuous observe→classify→act until ctx is done.
func StartSentinelLoop(ctx context.Context, args SentinelLoopArgs) {
	if args.Server == nil {
		return
	}
	interval := args.Interval
	if interval <= 0 {
		interval = DefaultSentinelInterval
	}
	if interval < 0 {
		<-ctx.Done()
		return
	}
	args.Server.ensureSentinelRuntime(args.MaxActionsPerHour)

	// Short first delay so boot is not blocked on a full interval.
	firstDelay := interval
	if firstDelay > 30*time.Second {
		firstDelay = 30 * time.Second
	}
	first := time.NewTimer(firstDelay)
	defer first.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("sentinel: continuous loop started",
		"interval", interval.String(),
		"max_actions_per_hour", args.MaxActionsPerHour,
		"dry_run", args.DryRun,
	)

	run := func(reason string) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("sentinel: cycle panic", "recover", r, "reason", reason)
			}
		}()
		// 🎯T359: the sentinel is load-bearing background — it keeps running
		// while ambient work defers, and stands down only when capacity has
		// narrowed to owner and Build work.
		verdict, release := capacity.Ask(args.Server.CapacityGovernor(), capacity.ClassControlRepair, reason)
		defer release()
		if !verdict.Admitted() {
			slog.Info("sentinel: cycle deferred by capacity",
				"reason", reason, "verdict", verdict.Verdict, "cause", verdict.Reason,
				"pressure", verdict.Pressure.String(), "detail", verdict.Detail)
			return
		}
		res, act := args.Server.runSentinelCycle(args)
		slog.Info("sentinel: cycle",
			"reason", reason,
			"primary", string(res.Primary),
			"signals", len(res.Decisions),
			"filed", len(res.FiledSymptoms),
			"repair", len(res.RepairSymptoms),
			"repaired", act.Repaired,
			"filed_po", act.FiledToPO,
			"delivered", act.Delivered,
			"skipped", act.Skipped,
		)
		if args.OnResult != nil {
			args.OnResult(res, act)
		}
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("sentinel: loop stopped")
			return
		case <-first.C:
			run("boot")
		case <-ticker.C:
			run("tick")
		}
	}
}

// runSentinelCycle samples product surfaces, classifies, optionally acts.
func (s *Server) runSentinelCycle(args SentinelLoopArgs) (staffops.CycleResult, SentinelActResult) {
	nowFn := args.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	now := nowFn()
	rt := s.ensureSentinelRuntime(args.MaxActionsPerHour)

	signals, resources := s.sampleSentinel(args, now)

	rt.mu.Lock()
	// Share cooldown with staff ops when present (same symptom suppress).
	cd := &rt.cooldown
	if s.staffOps != nil {
		// Prefer staffOps map if both exist — merge by using sentinel's
		// and copying staffOps last-file stamps in.
		for k, v := range s.staffOps.cooldown.LastFile {
			if prev, ok := cd.LastFile[k]; !ok || v.After(prev) {
				if cd.LastFile == nil {
					cd.LastFile = make(map[string]time.Time)
				}
				cd.LastFile[k] = v
			}
		}
	}
	res := staffops.RunCycle(staffops.CycleArgs{
		Signals:   signals,
		Resources: resources,
		Cooldown:  cd,
		Budget:    &rt.budget,
		Now:       now,
		DryRun:    args.DryRun,
		Sentinel:  true,
	})
	// Mirror cooldown back to staffOps.
	if s.staffOps != nil && !args.DryRun {
		s.staffOps.mu.Lock()
		if s.staffOps.cooldown.LastFile == nil {
			s.staffOps.cooldown.LastFile = make(map[string]time.Time)
		}
		for k, v := range cd.LastFile {
			s.staffOps.cooldown.LastFile[k] = v
		}
		s.staffOps.mu.Unlock()
	}
	rt.lastPrimary = res.Primary
	rt.lastTick = now
	rt.cycles++
	rt.mu.Unlock()

	act := SentinelActResult{}
	if args.DryRun {
		act.Skipped = "dry_run"
		s.logLifecycle(compSentinel, "cycle", "ok", map[string]any{
			"primary": string(res.Primary),
			"signals": len(res.Decisions),
			"dry_run": true,
		})
		return res, act
	}

	// Act on classification (control-plane only).
	switch res.Primary {
	case staffops.ActionRepair:
		s.TriggerFleetRecoverSweep()
		s.TriggerIdleNudgeSweep()
		overseer := args.Overseer
		if overseer == "" {
			overseer = s.overseerName()
		}
		s.SweepFleetHealth(overseer)
		act.Repaired = true
		act.AuditNote = "control-plane repair: fleet recover + idle nudge + dead-handle health"
		s.logLifecycle(compSentinel, "repair", "ok", map[string]any{
			"symptoms": res.RepairSymptoms,
			"primary":  string(res.Primary),
		})
		// Audit brief to overseer (not monologue — compact).
		_ = s.deliverSentinel(overseer, staffops.SentinelEventSource, res.WireText)
		act.Delivered = true

	case staffops.ActionFilePO:
		po := args.DefaultPO
		if po == "" {
			po = defaultProductPOName
		}
		overseer := args.Overseer
		if overseer == "" {
			overseer = s.overseerName()
		}
		mission := staffops.FormatPOMission(res)
		// Mission to PO (spawn-only path for residual Build).
		if err := s.deliverSentinel(po, staffops.SentinelEventSource, mission); err != nil {
			act.AuditNote = "PO deliver failed: " + err.Error()
			s.logLifecycle(compSentinel, "file_po", "error", map[string]any{
				"err": err.Error(), "symptoms": res.FiledSymptoms,
			})
		} else {
			act.FiledToPO = true
			act.Delivered = true
		}
		// Overseer gets full report for ledger file decision (alter-ego).
		_ = s.deliverSentinel(overseer, staffops.SentinelEventSource, res.WireText+"\n"+mission)
		act.Delivered = true
		s.logLifecycle(compSentinel, "file_po", "ok", map[string]any{
			"symptoms": res.FiledSymptoms,
			"po":       po,
			"primary":  string(res.Primary),
		})
		act.AuditNote = "file+PO: mission to " + po + "; report to overseer (no product implement; no Ship)"

	case staffops.ActionHarnessOK, staffops.ActionIgnore:
		act.Skipped = "no act — " + string(res.Primary)
		s.logLifecycle(compSentinel, "cycle", "ok", map[string]any{
			"primary": string(res.Primary),
			"signals": len(res.Decisions),
			"act":     "none",
		})
	default:
		act.Skipped = "unknown primary " + string(res.Primary)
	}

	return res, act
}

func (s *Server) deliverSentinel(target, event, text string) error {
	if strings.TrimSpace(target) == "" {
		return fmt.Errorf("sentinel deliver: empty target")
	}
	body := butler.FormatEventPush(event, text)
	if _, err := s.sendToAgent(target, body, false); err != nil {
		if s.butler != nil {
			if _, err2 := s.butler.PushEvent(target, event, text); err2 != nil {
				return fmt.Errorf("sentinel deliver send: %w; push: %v", err, err2)
			}
			return nil
		}
		if s.notifyJevon != nil {
			s.notifyJevon(body)
			return nil
		}
		return err
	}
	return nil
}

// sampleSentinel builds pure observe inputs from product APIs/logs.
func (s *Server) sampleSentinel(args SentinelLoopArgs, now time.Time) ([]staffops.Signal, staffops.ResourceSnapshot) {
	window := args.EventWindow
	if window <= 0 {
		window = DefaultSentinelEventWindow
	}
	grace := DefaultSentinelMechanicalGrace
	rt := s.ensureSentinelRuntime(args.MaxActionsPerHour)

	// 🎯T414: the intent snapshot travels with the sample, so every signal
	// this tick builds carries the deliberate answer to "should this be
	// running?" and staffops.Classify can decline a repair on it. Read once:
	// a tick that changed its mind halfway through would classify two halves
	// of one fleet against two different intents.
	intent := s.fleetIntent()
	in := staffops.ObserveInput{
		FleetIntent: intent.FleetState(),
		AgentIntent: map[string]fleetintent.State{},
	}
	resources := staffops.ResourceSnapshot{}

	overseer := args.Overseer
	if overseer == "" {
		overseer = s.overseerName()
	}

	// --- Overseer usability ---
	if s.registry != nil {
		def := s.registry.Def(overseer)
		proc := s.registry.Get(overseer)
		alive := proc != nil && proc.Alive()
		in.OverseerAlive = alive
		// Attached: def present + alive is enough for pure sample; cockpit owns attach.
		in.OverseerAttached = def != nil && alive
		// Stuck-busy residual: cockpit T204 owns unstick; sentinel only observes
		// via activity phase for reporting, not thrash.
		if alive && s.idleActivity != nil {
			phase := s.idleActivity.Get(overseer).Phase
			if phase == "busy" || phase == "working" {
				_ = phase
			}
		}
		// Grace for overseer_down: firstSeen bookkeeping.
		if !alive {
			rt.mu.Lock()
			fs, ok := rt.firstSeen["overseer:down"]
			if !ok {
				rt.firstSeen["overseer:down"] = now
				fs = now
			}
			in.OverseerGraceDone = now.Sub(fs) >= grace
			// If overseer was alive last cycle, firstSeen resets when alive —
			// handled below when alive.
			rt.mu.Unlock()
		} else {
			rt.mu.Lock()
			delete(rt.firstSeen, "overseer:down")
			rt.mu.Unlock()
		}
	} else {
		resources.Note = "registry unavailable"
	}

	// --- Fleet agents ---
	if s.registry != nil {
		reps := SweepDeadAgents(s.registry, overseer, intent)
		recovered := map[string]bool{}
		dead := map[string]DeadAgentReport{}
		for _, r := range reps {
			dead[r.Name] = r
			if r.Recovered {
				recovered[r.Name] = true
			}
		}

		running, stopped, idlePO := 0, 0, 0
		for _, d := range s.registry.List() {
			if d.Name == "" || d.Name == overseer {
				continue
			}
			proc := s.registry.Get(d.Name)
			alive := proc != nil && proc.Alive()
			if alive {
				running++
			} else {
				stopped++
				if isPOName(d.Name) {
					idlePO++
				}
			}

			ao := staffops.AgentObs{
				Name:  d.Name,
				Alive: alive,
			}
			in.AgentIntent[d.Name] = intent.AgentState(d.Name)
			// Deliberate stop: not AutoStart and not alive and no dead handle recover path.
			// Heuristic: purpose work, no process, Materialized but not AutoStart → stop OK.
			if !alive && !d.AutoStart && d.Purpose != claudia.PurposeOverseer {
				// Only mark deliberate when not in dead-handle report (handle cleared).
				if _, isDead := dead[d.Name]; !isDead {
					ao.DeliberateStop = true
				}
			}
			if r, ok := dead[d.Name]; ok {
				ao.DeadHandle = true
				ao.HarnessActed = r.Recovered
				sym := "dead:" + d.Name
				rt.mu.Lock()
				fs, seen := rt.firstSeen[sym]
				if !seen {
					rt.firstSeen[sym] = now
					fs = now
				}
				ao.GraceElapsed = now.Sub(fs) >= grace || r.Recovered
				rt.mu.Unlock()
				if r.Error != "" {
					ao.Detail = r.Error
				}
			}
			// Idle residue via the 🎯T423 decoder, not ACP absence.
			if alive {
				decoded := ClassifyAgentSessionPhase(d, DefaultSessionRoots())
				ao.Phase = decoded.String()
				openMission := strings.TrimSpace(d.TargetID) != ""
				ao.OpenMission = openMission
				if decoded == turnev.PhaseIdle && openMission {
					ao.IdleResidue = true
					sym := "idle:" + d.Name
					rt.mu.Lock()
					fs, seen := rt.firstSeen[sym]
					if !seen {
						rt.firstSeen[sym] = now
						fs = now
					}
					ao.GraceElapsed = now.Sub(fs) >= grace
					// HarnessActed unknown without nudge ledger — leave false until grace.
					rt.mu.Unlock()
				} else {
					rt.mu.Lock()
					delete(rt.firstSeen, "idle:"+d.Name)
					rt.mu.Unlock()
				}
			}
			in.Agents = append(in.Agents, ao)
		}
		resources.RunningAgents = running
		resources.StoppedAgents = stopped
		resources.IdlePOCount = idlePO
	}

	// --- Eventlog anomalies ---
	s.mu.Lock()
	tailFn := s.eventLogTail
	s.mu.Unlock()
	var eventRows []staffops.EventRow
	if tailFn != nil {
		evs, _, err := tailFn(eventlog.TailOptions{Limit: 200})
		if err == nil {
			eventRows = make([]staffops.EventRow, 0, len(evs))
			for _, e := range evs {
				eventRows = append(eventRows, eventRowFromEvent(e))
			}
			in.Events = staffops.ClusterEventAnomalies(eventRows, now, window)
		}
	} else if args.StateDir != "" {
		// Direct file tail when tool seam not wired (daemon always has path).
		path := eventlog.DefaultPath(args.StateDir)
		if evs, err := eventlog.Tail(path, eventlog.TailOptions{Limit: 200}); err == nil {
			eventRows = make([]staffops.EventRow, 0, len(evs))
			for _, e := range evs {
				eventRows = append(eventRows, eventRowFromEvent(e))
			}
			in.Events = staffops.ClusterEventAnomalies(eventRows, now, window)
		}
	}

	// 🎯T407: can the fleet run? Pause and provider walls are daemon-held;
	// ready-leaf count is not consulted here. Apply the same event window
	// ClusterEventAnomalies uses — an old quota row must not keep the
	// fleet blocked after the wall lifts.
	cut := now.Add(-window)
	recent := make([]staffops.EventRow, 0, len(eventRows))
	for _, r := range eventRows {
		if !r.TS.IsZero() && r.TS.Before(cut) {
			continue
		}
		recent = append(recent, r)
	}
	block := staffops.ClassifyFleetRun(staffops.FleetRunEvidence{
		AutoSpawnPaused:  args.AutoSpawnPaused || s.AutoSpawnPaused(),
		ProviderFailures: staffops.CollectProviderFailures(recent),
	})
	in.FleetBlock = block.AsObs()
	if !block.Runnable {
		if resources.Note != "" {
			resources.Note += "; "
		}
		resources.Note += "fleet blocked: " + block.Cause
		if block.Detail != "" {
			resources.Note += " (" + block.Detail + ")"
		}
	}

	// --- Frontier stall (🎯T346) ---
	// Use ClassifyLeaf / LeafReady count, not raw graph frontier depth.
	// Design-gated / deferred / parked / needs-owner / set_aside-parent /
	// high-infra hubs must not fire stall:frontier file+PO thrash.
	workdir := args.Workdir
	if workdir == "" {
		// Best-effort: common state is process cwd or empty.
		if wd, err := os.Getwd(); err == nil {
			workdir = wd
		}
	}
	if workdir != "" {
		if leaves, err := loadFrontierLeaves(workdir); err == nil {
			obs := make([]poproactive.LeafObs, 0, len(leaves))
			for _, leaf := range leaves {
				obs = append(obs, poproactive.LeafObs{
					ID:             leaf.ID,
					Tags:           leaf.Tags,
					Name:           leaf.Name,
					Context:        leaf.Context,
					Cost:           leaf.Cost,
					SetAsideDeps:   leaf.SetAsideDeps,
					ActiveChildren: leaf.ActiveChildren,
					ForceEngage:    poproactive.IsForceEngageTag(leaf.Tags),
					AlreadyEngaged: len(workAgentsEngagedOnTarget(s.registry, leaf.ID, workdir, "")) > 0,
				})
			}
			readyIDs := poproactive.Classify(obs).ReadyIDs
			engaged := 0
			if s.registry != nil {
				for _, d := range s.registry.List() {
					if d.Purpose != claudia.PurposeWork {
						continue
					}
					if strings.TrimSpace(d.TargetID) == "" {
						continue
					}
					if proc := s.registry.Get(d.Name); proc != nil && proc.Alive() {
						engaged++
					}
				}
			}
			depth, stalled, detail := staffops.FrontierStallObsWithIDs(readyIDs, engaged)
			in.FrontierDepth = depth
			in.FrontierStalled = stalled
			in.FrontierDetail = detail
			resources.FrontierDepth = depth

			// --- PO fan-out fault (🎯T380) ---
			// Same leaves, read against the POs answerable for them: a stall
			// on this frontier has an owner, and silence from that owner is a
			// fault rather than the sleep 🎯T325.1 blesses.
			in.POFanout = s.samplePOFanout(rt, obs, overseer, workdir, now, grace, args.ProcessRunning)
		}
	}

	// --- Cost alerts ---
	s.mu.Lock()
	snapFn := s.costSnapshot
	s.mu.Unlock()
	if snapFn != nil {
		if snap, err := snapFn(); err == nil && snap != nil {
			resources.SessionCount = len(snap.Sessions)
			resources.GlobalUSDPerHour = snap.GlobalUSDPerHour
			resources.FleetUSDPerHour = snap.FleetUSDPerHour
			resources.SpentTodayUSD = snap.SpentTodayUSD
			resources.Accounting = snap.Accounting
			for _, a := range snap.Alerts {
				sev := "medium"
				if a.Level >= cost.LevelPause {
					sev = "critical"
				}
				in.CostAlerts = append(in.CostAlerts, staffops.CostObs{
					Kind: a.Kind, Detail: a.Detail, Severity: sev,
				})
			}
		} else if err != nil {
			if resources.Note != "" {
				resources.Note += "; "
			}
			resources.Note += "cost snapshot error"
		}
	} else {
		if resources.Note != "" {
			resources.Note += "; "
		}
		resources.Note += "cost monitor off"
	}

	return staffops.BuildSignals(in), resources
}

// eventRowFromEvent projects an eventlog row into the pure clustering shape.
// Source and the fields.drill marker travel with it so synthetic RSI drill
// stimulus can be ignored rather than classified as a product anomaly (🎯T352).
func eventRowFromEvent(e eventlog.Event) staffops.EventRow {
	row := staffops.EventRow{
		Msg: e.Msg, Component: e.Component,
		Decision: e.Decision, Level: e.Level, Source: e.Source,
	}
	switch v := e.Fields["drill"].(type) {
	case bool:
		row.Drill = v
	case string:
		row.Drill = strings.EqualFold(strings.TrimSpace(v), "true")
	}
	if fc, ok := e.Fields["failure_class"].(string); ok {
		row.FailureClass = strings.TrimSpace(fc)
	}
	if e.TS != "" {
		if t, err := time.Parse(time.RFC3339Nano, e.TS); err == nil {
			row.TS = t
		} else if t, err := time.Parse(time.RFC3339, e.TS); err == nil {
			row.TS = t
		}
	}
	return row
}

func loadFrontierLeaves(workdir string) ([]targetfile.FrontierLeaf, error) {
	path := filepath.Join(workdir, "bullseye.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return targetfile.FrontierLeaves(data)
}
