// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/poproactive"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// 🎯T254.1: daemon-enforced unattended frontier consumption. Every
// non-design-gated ready frontier leaf gets an engaged work agent under the
// product PO within one operational cycle, or an explicit park reason — with
// no owner spawn command and no dependence on the PO LLM noticing.
//
// Composes: T155/T193 skip set (poproactive design-gate markers), T222
// engagement (registered work agent bound to the target ⇒ skip), T129
// lineage (parent=jevons-po; PO unregistered ⇒ park, rehydrate is not this
// sweep's job), T36 budget clamp (spawnGuard parks the whole cycle), T197
// literal dots in worker names. 🎯T337: park leaves unblocked only via
// set_aside deps (T7→T5) and optional high-cost mobile/iPad class unless
// unattended-safe / force-engage. 🎯T338: park parents with active hierarchical
// children (T10 with T10.2–T10.6) and optional high-infra class (sqlpipe/CGO/Peer,
// cost≥13 multi-child) unless force-engage / unattended-safe. 🎯T339: park
// deferred / not-urgent / later-device leaves (T22 voice class), T28-class
// car/iPad/VoicelabKit device DSP (🎯T342), and T29-class generative-UI /
// DEFERRED future-work ambition (🎯T343) with skip_deferred or owner-parked
// unless force-engage / unattended-safe. 🎯T449: park leaves the ledger
// records as built-and-awaiting-the-owner's-verdict (owned_by the owner with
// a landed-code reason), and leaves assigned to another driver — the only
// park class that means the work is DONE rather than unstarted.
// Anti-thrash:
// durable per-target spawn ledger (backoff + max auto-spawns) so a worker that
// finishes without achieving cannot churn the same leaf forever.
//
// Park reasons are lifecycle-logged (eventJournal → GET /api/logs), which is
// the thin owner-visible chrome; richer frontier-table chrome is residual.

const (
	// DefaultFrontierConsumeInterval is how often the daemon re-evaluates the
	// frontier for unconsumed leaves. Conservative drip: with the per-cycle
	// cap below, a cold backlog consumes at ≤ 6 workers/hour.
	DefaultFrontierConsumeInterval = 10 * time.Minute
	// DefaultFrontierConsumeMaxSpawnsPerCycle bounds spawns per sweep.
	DefaultFrontierConsumeMaxSpawnsPerCycle = 1
	// DefaultFrontierConsumeMaxSpawnsPerTarget bounds automatic re-spawns for
	// one leaf across daemon lifetime (durable ledger). A leaf that burns its
	// budget parks with max_autospawns until an achieve or owner action.
	DefaultFrontierConsumeMaxSpawnsPerTarget = 3
	// DefaultFrontierConsumeRespawnBackoff is the minimum wait between
	// auto-spawns for the same target (worker died / finished-not-achieved).
	DefaultFrontierConsumeRespawnBackoff = 30 * time.Minute

	compFrontierConsume = "frontier_consume"
)

// FrontierConsumeAction is the sweep decision for one frontier leaf.
type FrontierConsumeAction string

const (
	// FrontierConsumeSpawn: a worker was (or would be) auto-spawned.
	FrontierConsumeSpawn FrontierConsumeAction = "spawn"
	// FrontierConsumePark: leaf stays unconsumed with an explicit reason.
	FrontierConsumePark FrontierConsumeAction = "park"
	// FrontierConsumeSkip: leaf needs no daemon action (engaged / gated).
	FrontierConsumeSkip FrontierConsumeAction = "skip"
)

// Park / skip reason codes (stable for logs and tests).
const (
	FrontierReasonSpawned              = "spawned"
	FrontierReasonDesignGated          = "skip_design"
	FrontierReasonBlocked              = "skip_blocked"
	FrontierReasonEngaged              = "skip_engaged"
	FrontierReasonClosed               = "skip_closed"
	FrontierReasonSetAsideDep          = "skip_set_aside_dep"               // 🎯T337 T7→T5 class
	FrontierReasonHighCostMobile       = "skip_high_cost_mobile"            // 🎯T337 mobile megawork
	FrontierReasonParentActiveChildren = "skip_parent_with_active_children" // 🎯T338 T10 parent class
	FrontierReasonHighInfra            = "skip_high_infra"                  // 🎯T338 sqlpipe/CGO/Peer
	// FrontierReasonAwaitingOwnerVerdict parks a leaf whose code has landed
	// and whose sole residue is the owner's taste verdict (🎯T449). Every
	// other park reason describes work that has not started; this one is the
	// opposite, and the owner reading a parked frontier has to be able to
	// tell "finished, your turn" from "nobody has touched this".
	FrontierReasonAwaitingOwnerVerdict = "park_awaiting_owner_verdict" // 🎯T449
	FrontierReasonOwnedByOther         = "park_owned_by"               // 🎯T449 assigned to another driver
	FrontierReasonDeferred             = "skip_deferred"               // 🎯T339/T342/T343 not-urgent + device-voice + T29 ambition
	FrontierReasonOwnerParked          = "owner-parked"                // 🎯T339 explicit owner-parked
	FrontierReasonCapacity             = "park_capacity"
	FrontierReasonBackoff              = "park_backoff"
	FrontierReasonMaxAutospawns        = "park_max_autospawns"
	FrontierReasonPOMissing            = "park_po_unregistered"
	FrontierReasonSpawnHalted          = "park_spawn_halted"
	FrontierReasonSpawnFailed          = "park_spawn_failed"
	// FrontierReasonFleetIntent parks every ready leaf because the fleet is
	// deliberately not working (🎯T414). Distinct from park_spawn_halted,
	// which is the budget clamp: an owner reading a parked frontier has to be
	// able to tell "I stood this fleet down" from "the clamp tripped".
	FrontierReasonFleetIntent = "park_fleet_intent"
)

// FrontierConsumeReport is one leaf's sweep outcome.
type FrontierConsumeReport struct {
	TargetID string
	Action   FrontierConsumeAction
	Reason   string
	Worker   string // spawned worker name when Action=spawn
	Err      string
}

// FrontierWorkerName derives the auto-spawn worker name for a target id.
// Hierarchical ids keep literal dots (🎯T197): T254.1 → jv-t254.1-auto.
func FrontierWorkerName(targetID string) string {
	id := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(targetID, "🎯")))
	if id == "" {
		return ""
	}
	return "jv-" + id + "-auto"
}

// FormatFrontierSpawnBrief builds the opening mission brief for an
// auto-spawned worker. The standing fleet brief is injected once by the
// deliverStartPrompt → EnsureFleetBrief path, so this carries mission only.
func FormatFrontierSpawnBrief(leaf targetfile.FrontierLeaf, parent string) string {
	var b strings.Builder
	tid := FormatTargetID(leaf.ID)
	fmt.Fprintf(&b, "%s — %s\n\n", tid, leaf.Name)
	b.WriteString("You were auto-spawned by the jevonsd frontier-consumption sweep (🎯T254.1): ")
	fmt.Fprintf(&b, "this frontier leaf had no engaged implementer. Parent: %s.\n\n", parent)
	if len(leaf.Acceptance) > 0 {
		b.WriteString("## Acceptance\n")
		for _, a := range leaf.Acceptance {
			fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(a))
		}
		b.WriteString("\n")
	}
	if leaf.Context != "" {
		fmt.Fprintf(&b, "## Context\n%s\n\n", leaf.Context)
	}
	b.WriteString("Local master only (🎯T104) — no PR, no push. ")
	b.WriteString("Finish: commit SHA + green oracle evidence reported to your parent (or explicit accepted-risk). ")
	b.WriteString("If the leaf turns out design-gated or needs the owner, report blocked with the reason instead of guessing.")
	return b.String()
}

// frontierOwnerGateNote renders the park detail for an awaiting-owner-verdict
// leaf (🎯T449): the recorded reason if the ledger has one, else the bare
// fact of the assignment. Truncated, because the reason carries the whole
// gated report and the park line lands in a log.
func frontierOwnerGateNote(leaf poproactive.LeafObs) string {
	const maxNote = 240
	note := strings.TrimSpace(leaf.OwnedByReason)
	if note == "" {
		note = "assigned to " + strings.TrimSpace(leaf.OwnedBy) + "; awaiting owner verdict"
	}
	if len(note) > maxNote {
		note = strings.TrimSpace(note[:maxNote]) + "…"
	}
	return note
}

// --- durable per-target spawn ledger (backoff / max across restarts) ---

type frontierSpawnLedgerFile struct {
	Targets map[string]frontierSpawnEntry `json:"targets"`
}

type frontierSpawnEntry struct {
	Count     int       `json:"count"`
	LastSpawn time.Time `json:"last_spawn"`
}

// FrontierSpawnLedger persists per-target auto-spawn counts under state_dir.
type FrontierSpawnLedger struct {
	path string
	mu   sync.Mutex
	data frontierSpawnLedgerFile
}

// OpenFrontierSpawnLedger loads or creates state_dir/fleet/frontier_consume.json.
func OpenFrontierSpawnLedger(stateDir string) (*FrontierSpawnLedger, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, fmt.Errorf("frontier consume: StateDir required")
	}
	dir := filepath.Join(stateDir, "fleet")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "frontier_consume.json")
	l := &FrontierSpawnLedger{path: path, data: frontierSpawnLedgerFile{Targets: map[string]frontierSpawnEntry{}}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return l, nil
		}
		return nil, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &l.data); err != nil {
			return nil, fmt.Errorf("frontier consume ledger: %w", err)
		}
	}
	if l.data.Targets == nil {
		l.data.Targets = map[string]frontierSpawnEntry{}
	}
	return l, nil
}

// Get returns count and last auto-spawn time for a target id.
func (l *FrontierSpawnLedger) Get(targetID string) (count int, last time.Time) {
	if l == nil {
		return 0, time.Time{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.data.Targets[targetID]
	return e.Count, e.LastSpawn
}

// Record increments count and stamps last spawn; atomic write-and-rename.
func (l *FrontierSpawnLedger) Record(targetID string, at time.Time) error {
	if l == nil || targetID == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.data.Targets == nil {
		l.data.Targets = map[string]frontierSpawnEntry{}
	}
	e := l.data.Targets[targetID]
	e.Count++
	e.LastSpawn = at.UTC()
	l.data.Targets[targetID] = e
	b, err := json.MarshalIndent(l.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

// --- pure sweep ---

// FrontierConsumeArgs configures one sweep evaluation. Leaves carry the
// caller-assembled observations (engagement, blocked); Spawn performs the
// actual worker launch (production: stitch+Launch+brief; tests: fake).
type FrontierConsumeArgs struct {
	Leaves []poproactive.LeafObs
	// SpawnCount / SinceLastSpawn come from the durable ledger; nil-safe via
	// Ledger. When Ledger is nil, every leaf looks never-spawned.
	Ledger *FrontierSpawnLedger
	Spawn  func(leaf poproactive.LeafObs, workerName string) error
	Now    time.Time
	// PORegistered is false when the product PO is not in the registry —
	// every ready leaf parks (T129: rehydrate PO first, not bypass lineage).
	PORegistered bool
	// SpawnHalted carries the budget-clamp refusal when non-empty (T36):
	// every ready leaf parks with park_spawn_halted.
	SpawnHalted string
	// FleetIntent is the deliberate fleet-wide intent (🎯T414). Anything but
	// working parks every ready leaf: a ready leaf is a fact about the
	// ledger, never a licence to start a worker. This is the control that
	// spawned against 34 ready leaves it read as neglect while the fleet was
	// parked under a provider block.
	FleetIntent fleetintent.State
	// MaxSpawnsPerCycle 0 → DefaultFrontierConsumeMaxSpawnsPerCycle.
	MaxSpawnsPerCycle int
	// MaxSpawnsPerTarget 0 → DefaultFrontierConsumeMaxSpawnsPerTarget.
	MaxSpawnsPerTarget int
	// RespawnBackoff 0 → DefaultFrontierConsumeRespawnBackoff.
	RespawnBackoff time.Duration
}

// SweepFrontierConsume evaluates every leaf and auto-spawns up to the
// per-cycle cap. Every leaf gets a report: spawn, park (explicit reason), or
// skip (engaged / gated / blocked / closed). Hermetic oracle for 🎯T254.1.
func SweepFrontierConsume(args FrontierConsumeArgs) []FrontierConsumeReport {
	now := args.Now
	if now.IsZero() {
		now = time.Now()
	}
	maxCycle := args.MaxSpawnsPerCycle
	if maxCycle <= 0 {
		maxCycle = DefaultFrontierConsumeMaxSpawnsPerCycle
	}
	maxTarget := args.MaxSpawnsPerTarget
	if maxTarget <= 0 {
		maxTarget = DefaultFrontierConsumeMaxSpawnsPerTarget
	}
	backoff := args.RespawnBackoff
	if backoff <= 0 {
		backoff = DefaultFrontierConsumeRespawnBackoff
	}

	var out []FrontierConsumeReport
	spawned := 0
	for _, leaf := range args.Leaves {
		id := strings.TrimSpace(leaf.ID)
		if id == "" {
			continue
		}
		rep := FrontierConsumeReport{TargetID: id}
		switch poproactive.ClassifyLeaf(leaf) {
		case poproactive.LeafSkipAwaitingOwnerVerdict:
			// Park, loudly: the leaf is finished and the ball is in the
			// owner's court, so the reason carries the recorded claim
			// rather than a bare code (🎯T449).
			rep.Action, rep.Reason = FrontierConsumePark, FrontierReasonAwaitingOwnerVerdict
			rep.Err = frontierOwnerGateNote(leaf)
			out = append(out, rep)
			continue
		case poproactive.LeafSkipOwnedByOther:
			rep.Action, rep.Reason = FrontierConsumePark, FrontierReasonOwnedByOther
			rep.Err = "assigned to " + strings.TrimSpace(leaf.OwnedBy)
			out = append(out, rep)
			continue
		case poproactive.LeafSkipDesign:
			rep.Action, rep.Reason = FrontierConsumeSkip, FrontierReasonDesignGated
			out = append(out, rep)
			continue
		case poproactive.LeafSkipSetAsideDep:
			// Park (not skip): explicit reason owner-visible — leaf stays
			// unconsumed until force_engage / dep reopened (🎯T337).
			rep.Action, rep.Reason = FrontierConsumePark, FrontierReasonSetAsideDep
			if len(leaf.SetAsideDeps) > 0 {
				rep.Err = "set_aside deps: " + strings.Join(leaf.SetAsideDeps, ",")
			}
			out = append(out, rep)
			continue
		case poproactive.LeafSkipParentActiveChildren:
			// Park: prefer ready child leaves over parent mega-mission (🎯T338).
			rep.Action, rep.Reason = FrontierConsumePark, FrontierReasonParentActiveChildren
			if len(leaf.ActiveChildren) > 0 {
				rep.Err = "parent-with-active-children: " + strings.Join(leaf.ActiveChildren, ",")
			}
			out = append(out, rep)
			continue
		case poproactive.LeafSkipHighCostMobile:
			rep.Action, rep.Reason = FrontierConsumePark, FrontierReasonHighCostMobile
			out = append(out, rep)
			continue
		case poproactive.LeafSkipHighInfra:
			rep.Action, rep.Reason = FrontierConsumePark, FrontierReasonHighInfra
			out = append(out, rep)
			continue
		case poproactive.LeafSkipDeferred:
			// Park: deferred / not-urgent / later-device without owner open (🎯T339).
			// Prefer owner-parked when the leaf is explicitly tagged/phrased that way.
			reason := FrontierReasonDeferred
			if poproactive.IsOwnerParkedTag(leaf.Tags) ||
				strings.Contains(strings.ToLower(leaf.Name+" "+leaf.Context), "owner-parked") {
				reason = FrontierReasonOwnerParked
			}
			rep.Action, rep.Reason = FrontierConsumePark, reason
			rep.Err = "deferred/not-urgent without owner open"
			out = append(out, rep)
			continue
		case poproactive.LeafSkipBlocked:
			rep.Action, rep.Reason = FrontierConsumeSkip, FrontierReasonBlocked
			out = append(out, rep)
			continue
		case poproactive.LeafSkipEngaged:
			rep.Action, rep.Reason = FrontierConsumeSkip, FrontierReasonEngaged
			out = append(out, rep)
			continue
		case poproactive.LeafSkipClosed:
			rep.Action, rep.Reason = FrontierConsumeSkip, FrontierReasonClosed
			out = append(out, rep)
			continue
		}
		// Ready leaf: enforcement path.
		//
		// 🎯T414 first, ahead of the budget clamp: when both are set the
		// intent is the more useful answer, because only the owner can lift
		// it and the clamp will still be there when they do.
		if dec := fleetintent.AllowsFleet(args.FleetIntent, fleetintent.ControlSpawn); !dec.Allow {
			rep.Action, rep.Reason = FrontierConsumePark, FrontierReasonFleetIntent
			rep.Err = "fleet intent: " + fleetintent.Describe(dec.Blocking)
			out = append(out, rep)
			continue
		}
		if args.SpawnHalted != "" {
			rep.Action, rep.Reason = FrontierConsumePark, FrontierReasonSpawnHalted
			rep.Err = args.SpawnHalted
			out = append(out, rep)
			continue
		}
		if !args.PORegistered {
			rep.Action, rep.Reason = FrontierConsumePark, FrontierReasonPOMissing
			out = append(out, rep)
			continue
		}
		count, last := args.Ledger.Get(id)
		if count >= maxTarget {
			rep.Action, rep.Reason = FrontierConsumePark, FrontierReasonMaxAutospawns
			out = append(out, rep)
			continue
		}
		if !last.IsZero() && now.Sub(last) < backoff {
			rep.Action, rep.Reason = FrontierConsumePark, FrontierReasonBackoff
			out = append(out, rep)
			continue
		}
		if spawned >= maxCycle {
			rep.Action, rep.Reason = FrontierConsumePark, FrontierReasonCapacity
			out = append(out, rep)
			continue
		}
		worker := FrontierWorkerName(id)
		if args.Spawn == nil {
			rep.Action, rep.Reason = FrontierConsumePark, FrontierReasonSpawnFailed
			rep.Err = "no spawner"
			out = append(out, rep)
			continue
		}
		if err := args.Spawn(leaf, worker); err != nil {
			rep.Action, rep.Reason = FrontierConsumePark, FrontierReasonSpawnFailed
			rep.Err = err.Error()
			out = append(out, rep)
			continue
		}
		spawned++
		rep.Action, rep.Reason, rep.Worker = FrontierConsumeSpawn, FrontierReasonSpawned, worker
		if err := args.Ledger.Record(id, now); err != nil {
			slog.Warn("frontier consume ledger record failed", "target", id, "err", err)
		}
		out = append(out, rep)
	}
	return out
}

// --- daemon assembly ---

// FrontierConsumeLoopArgs wires the sweep into the daemon.
type FrontierConsumeLoopArgs struct {
	Server   *Server
	StateDir string
	// Workdir is the product repo used for ledger discovery and as the
	// spawned workers' workdir.
	Workdir string
	// ParentPO empty → jevons-po (sole spawn parent for product workers, T129).
	ParentPO string
	// Interval 0 → DefaultFrontierConsumeInterval; < 0 disables the ticker
	// (TriggerFrontierConsumeSweep remains callable).
	Interval           time.Duration
	MaxSpawnsPerCycle  int
	MaxSpawnsPerTarget int
	RespawnBackoff     time.Duration
	// Spawn overrides the production launch path (hermetic tests without a
	// provider process). Nil → Server.spawnFrontierWorker.
	Spawn func(leaf targetfile.FrontierLeaf, workerName, parent string) error
}

// StartFrontierConsumeLoop opens the durable spawn ledger and ticks the
// enforcement sweep until ctx is done (🎯T254.1).
func StartFrontierConsumeLoop(ctx context.Context, args FrontierConsumeLoopArgs) {
	if args.Server == nil || args.Server.registry == nil {
		return
	}
	var ledger *FrontierSpawnLedger
	if args.StateDir != "" {
		l, err := OpenFrontierSpawnLedger(args.StateDir)
		if err != nil {
			slog.Warn("frontier consume: ledger open failed (backoff/max not durable)", "err", err)
		} else {
			ledger = l
		}
	}
	interval := args.Interval
	if interval == 0 {
		interval = DefaultFrontierConsumeInterval
	}
	if interval < 0 {
		<-ctx.Done()
		return
	}
	slog.Info("frontier consume loop started",
		"interval", interval, "workdir", args.Workdir,
		"max_per_cycle", args.MaxSpawnsPerCycle, "max_per_target", args.MaxSpawnsPerTarget)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			args.Server.frontierConsumeSweep(args, ledger)
		}
	}
}

// frontierConsumeSweep assembles live observations and runs one enforcement
// pass: ledger leaves + registry engagement → spawn under the product PO.
func (s *Server) frontierConsumeSweep(args FrontierConsumeLoopArgs, ledger *FrontierSpawnLedger) []FrontierConsumeReport {
	if s == nil || s.registry == nil {
		return nil
	}
	parentPO := strings.TrimSpace(args.ParentPO)
	if parentPO == "" {
		parentPO = defaultProductPOName
	}
	leaves, ledgerPath, err := targetfile.LoadFrontierLeavesFromCwd(args.Workdir)
	if err != nil {
		slog.Warn("frontier consume: ledger load failed", "workdir", args.Workdir, "err", err)
		return nil
	}
	// 🎯T499: recency bias — the per-cycle spawn slot goes to the newest
	// ready leaf. Older ready leaves still sweep (park_capacity), so they
	// stay ready and engage once newer work is consumed or engaged.
	targetfile.OrderLeavesPreferRecent(leaves)

	byID := make(map[string]targetfile.FrontierLeaf, len(leaves))
	obs := make([]poproactive.LeafObs, 0, len(leaves))
	for _, leaf := range leaves {
		byID[leaf.ID] = leaf
		obs = append(obs, poproactive.LeafObs{
			ID:             leaf.ID,
			Tags:           leaf.Tags,
			Name:           leaf.Name,
			Context:        leaf.Context,
			Cost:           leaf.Cost,
			SetAsideDeps:   leaf.SetAsideDeps,
			ActiveChildren: leaf.ActiveChildren,
			OwnedBy:        leaf.OwnedBy,
			OwnedByReason:  leaf.OwnedByReason,
			ForceEngage:    poproactive.IsForceEngageTag(leaf.Tags),
			// 🎯T389: this sweep's ledger only — another repo's worker on the
			// same id must not make this leaf look consumed.
			AlreadyEngaged: len(workAgentsEngagedOnTarget(s.registry, leaf.ID, args.Workdir, "")) > 0,
		})
	}

	spawnHalted := ""
	if s.spawnGuard != nil {
		if err := s.spawnGuard(); err != nil {
			spawnHalted = err.Error()
		}
	}

	reps := SweepFrontierConsume(FrontierConsumeArgs{
		Leaves:             obs,
		Ledger:             ledger,
		Now:                time.Now(),
		PORegistered:       s.registry.Def(parentPO) != nil,
		SpawnHalted:        spawnHalted,
		FleetIntent:        s.fleetIntent().FleetState(),
		MaxSpawnsPerCycle:  args.MaxSpawnsPerCycle,
		MaxSpawnsPerTarget: args.MaxSpawnsPerTarget,
		RespawnBackoff:     args.RespawnBackoff,
		Spawn: func(leaf poproactive.LeafObs, workerName string) error {
			if args.Spawn != nil {
				return args.Spawn(byID[leaf.ID], workerName, parentPO)
			}
			return s.spawnFrontierWorker(workerName, args.Workdir, parentPO, leaf.ID,
				FormatFrontierSpawnBrief(byID[leaf.ID], parentPO))
		},
	})

	spawnedN, parkedN := 0, 0
	for _, r := range reps {
		switch r.Action {
		case FrontierConsumeSpawn:
			spawnedN++
			s.logLifecycle(compFrontierConsume, "spawn", "ok", map[string]any{
				"target_id": r.TargetID, "worker": r.Worker, "parent": parentPO,
			})
			s.notifyFleetHealth(fmt.Sprintf(
				"[frontier-consume 🎯T254.1] auto-spawned %s for 🎯%s under %s (unconsumed frontier leaf)",
				r.Worker, r.TargetID, parentPO))
		case FrontierConsumePark:
			parkedN++
			// park_capacity is normal drip pacing — log-noise budget: debug only.
			if r.Reason == FrontierReasonCapacity {
				continue
			}
			s.logLifecycle(compFrontierConsume, "park", "ok", map[string]any{
				"target_id": r.TargetID, "reason": r.Reason, "err": r.Err,
			})
			// 🎯T433: a spawn that failed is a leaf that lost its worker, and
			// the lifecycle journal is not a notification — the owning PO
			// hears about it by name, with the error verbatim, or the leaf
			// reads as unattended-but-fine forever (the T383 instance).
			if r.Reason == FrontierReasonSpawnFailed {
				s.notifySpawnFailure(parentPO, r.TargetID, FrontierWorkerName(r.TargetID), r.Err)
			}
		}
	}
	if spawnedN > 0 || parkedN > 0 {
		slog.Info("frontier consume sweep",
			"ledger", ledgerPath, "leaves", len(reps), "spawned", spawnedN, "parked", parkedN)
	}
	return reps
}

// FormatSpawnFailureNotice renders the PO-addressed account of a spawn whose
// worker never received its brief (🎯T433): the leaf by id, the worker by
// name, and the delivery error verbatim, plus what the daemon already did
// about it — so the PO can act without first reconstructing the failure from
// the lifecycle journal.
//
// Phrasing constraint: this text is delivered to a PO with OriginAgent, so it
// must classify relayroute.RouteParent — "blocked on", "needs owner", and
// done-with-oracle phrases would bounce it off the PO to the overseer with
// only a record line left behind (🎯T392.7), which is precisely the silence
// this notice exists to end. TestT433NoticeStaysOnThePO pins that. The error
// text is quoted verbatim regardless; a pathological error that trips the
// classifier still reaches the overseer in full, which is surfaced, not
// silent — accepted residual.
func FormatSpawnFailureNotice(targetID, worker, errText string) string {
	tid := FormatTargetID(targetID)
	if strings.TrimSpace(errText) == "" {
		errText = "no error detail was recorded"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[spawn-failure 🎯T433] %s has no worker: auto-spawn of %s FAILED — %s. ",
		tid, worker, strings.TrimSpace(errText))
	fmt.Fprintf(&b, "No briefless seat was retained; the leaf is parked %s and the sweep will retry. ",
		FrontierReasonSpawnFailed)
	fmt.Fprintf(&b, "If this repeats, start a worker for %s by hand or investigate the spawn path.", tid)
	return b.String()
}

// notifySpawnFailure surfaces one spawn failure to the owning product owner
// through the single deliver-by-name path (🎯T433). Repeat failures with the
// same error are surfaced once per daemon lifetime; a PO that could not be
// reached at all falls back to the overseer via notifyFleetHealth, because
// the one outcome this exists to prevent is the silent park.
func (s *Server) notifySpawnFailure(po, targetID, worker, errText string) {
	if s == nil || strings.TrimSpace(po) == "" {
		return
	}
	key := targetID + "|" + worker
	s.mu.Lock()
	if s.spawnFailureNotified == nil {
		s.spawnFailureNotified = map[string]string{}
	}
	if s.spawnFailureNotified[key] == errText {
		s.mu.Unlock()
		return
	}
	s.spawnFailureNotified[key] = errText
	s.mu.Unlock()

	msg := FormatSpawnFailureNotice(targetID, worker, errText)
	if _, err := s.deliverByName(po, msg, OriginAgent, false); err != nil {
		slog.Warn("spawn-failure notice undelivered to PO; escalating to overseer",
			"component", compFrontierConsume, "po", po, "target", targetID, "err", err)
		s.notifyFleetHealth(fmt.Sprintf("PO %s unreachable (%v) for: %s", po, err, msg))
	}
}

// spawnFrontierWorker launches one auto-spawn worker through the same stitch
// path as jevons_agent_start (provider routing, T222 gate already cleared by
// the sweep's engagement check). Cursor ACP (🎯T541) starts then sends the
// brief without holding startMu; other providers still confirm the opening
// brief on start (🎯T305).
func (s *Server) spawnFrontierWorker(name, workdir, parent, targetID, brief string) error {
	if name == "" || workdir == "" {
		return fmt.Errorf("worker name and workdir required")
	}
	def, existed, _, err := s.stitchAgentStart(name, workdir, "", "", "", parent, "work", normalizeAgentTargetID(targetID), brief)
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}
	s.startMu.Lock()
	proc, err := s.launchAgent(name)
	if err != nil {
		s.startMu.Unlock()
		return fmt.Errorf("launch: %w", err)
	}
	s.wireAgentEvents(name, proc)
	s.startMu.Unlock()
	if deferStartPrompt(def.Provider) {
		if _, err := s.finishCursorStart(name, existed, brief); err != nil {
			if _, kept := s.startBriefFailureTeardown(name, existed, err); kept {
				slog.Info("frontier consume: worker spawned, brief in flight",
					"worker", name, "target", targetID, "parent", parent,
					"provider", def.Provider, "detail", err.Error())
				return nil
			}
			return err
		}
		slog.Info("frontier consume: worker auto-spawned",
			"worker", name, "target", targetID, "parent", parent,
			"provider", def.Provider, "session", sessionDisplay(def.SessionID))
		return nil
	}
	if err := s.deliverStartPrompt(name, brief); err != nil {
		if _, kept := s.startBriefFailureTeardown(name, existed, err); kept {
			// 🎯T518: the brief is queued/undecided, not proven lost. The seat
			// stands and the leaf counts as consumed; reaping here is what let
			// remints race the sweep while the first brief sat in the queue.
			slog.Info("frontier consume: worker spawned, brief in flight",
				"worker", name, "target", targetID, "parent", parent,
				"provider", def.Provider, "detail", err.Error())
			return nil
		}
		// No silent phantom seat: stop the process so agent_list does not
		// report an unbriefed worker as consumption (same as agent_start),
		// and (🎯T387) release a row this sweep minted so the leaf stays
		// unconsumed for the next pass rather than being permanently masked
		// by a worker that never ran (teardown already released it above).
		return err
	}
	slog.Info("frontier consume: worker auto-spawned",
		"worker", name, "target", targetID, "parent", parent,
		"provider", def.Provider, "session", sessionDisplay(def.SessionID))
	return nil
}
