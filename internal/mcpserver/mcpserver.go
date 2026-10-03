// Copyright 2025 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package mcpserver exposes jevon worker management as MCP tools,
// replacing the jevon-ctl CLI binary with an in-process MCP server.
package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/audit"
	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/doit"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/mcpattach"
	"github.com/marcelocantos/jevons/internal/panecensus"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/reapverify"
	"github.com/marcelocantos/jevons/internal/research"
	"github.com/marcelocantos/jevons/internal/roles"
	"github.com/marcelocantos/jevons/internal/rsi"
	"github.com/marcelocantos/jevons/internal/seatload"
	"github.com/marcelocantos/jevons/internal/seatplan"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/seatstop"
	"github.com/marcelocantos/jevons/internal/secauditor"
	"github.com/marcelocantos/jevons/internal/sendq"
	"github.com/marcelocantos/jevons/internal/spawnorder"
	"github.com/marcelocantos/jevons/internal/turnev"
	"github.com/marcelocantos/jevons/internal/wakebatch"
	"github.com/marcelocantos/jevons/internal/workers"
	"github.com/marcelocantos/jevons/internal/writconf"
)

// ScreenshotFunc requests a screenshot from connected clients and returns the file path.
type ScreenshotFunc func() (string, error)

// TranscriptOps provides transcript manipulation functions.
type TranscriptOps struct {
	Read func(sessionID string) ([]map[string]any, error)
	// ReadForSeat prefers the dated sidecar spool for seat (🎯T866.4).
	ReadForSeat func(seat, sessionID string) ([]map[string]any, error)
	Truncate    func(sessionID string, keepTurns int) error
	GetID       func() string // current Jevon claude session ID (from claudia registry)
	// Locate answers where Read would look for sessionID: the resolved path
	// ("" when absent) and every candidate location searched. 🎯T597: a
	// not-found must name the paths it is a claim about.
	Locate func(sessionID string) (path string, searched []string)
}

// Server wraps an MCP server that provides worker management tools.
type Server struct {
	registry *claudia.Registry
	// seatPlans holds placement and migration fields the published
	// AgentDef does not carry. Nil reads as empty state.
	seatPlans  *seatplan.Store
	scanner    *discovery.Scanner
	butler     *butler.Butler
	workerWD   string
	screenshot ScreenshotFunc
	transcript *TranscriptOps

	// seatLoad anchors each seat's process group while its root is alive,
	// so a stop, a reap or a lost seat takes its detached background work
	// with it (🎯T708). Lazily built; see seat_load.go.
	seatLoadMu sync.Mutex
	seatLoad   *seatload.Tracker

	// seats is the single answer to "what is true about this seat"
	// (🎯T766.2). Controls read it instead of each deriving their own;
	// see docs/fleet-census.md for the eleven derivations it replaces.
	// Injected by SetSeats from main (the same instance internal/server and
	// internal/fleet hold); built lazily and privately only in tests.
	seatsOnce sync.Once
	seats     *seatstate.Authority

	// spawnGuard / resumeGuard are the budget clamp-down gates (T36.1):
	// every MCP path that creates or re-launches a worker must consult
	// them so spawnHalted cannot be bypassed via jwork / agent_start.
	// Nil means unguarded (tests / cost DB unavailable).
	spawnGuard  func() error
	resumeGuard func(id string, auto bool) error

	mcpSrv    *server.MCPServer
	transport *server.StreamableHTTPServer

	toolsListCount int64
	// toolCallObserver sees every HTTP tools/call name+args (🎯T64.2).
	toolCallObserver func(name string, args map[string]any)

	// bootAt is when this Server was created — the "last daemon restart"
	// baseline the 🎯T597 briefless-seat check reasons against.
	bootAt time.Time
	// seatMintedAt records, under mu, when this daemon (re-)minted each
	// seat's session (🎯T597). Absent name ⇒ minted before the last restart.
	seatMintedAt map[string]time.Time

	// grokSessions is the Grok session store (~/.grok/sessions), read under mu.
	// A Grok ACP seat's process reports no transcript path, so this is how the
	// read-back finds one (🎯T752).
	grokSessions string

	mu sync.Mutex
	// startMu serializes Launch/wire on jevons_agent_start. It must be
	// released before any ACP prompt delivery (🎯T541): holding it across
	// session/prompt confirmation hangs MCP so agent_list/send/kill/event_push
	// time out. Launch itself is also deadline-bounded (🎯T541.2) so a hung
	// session/load cannot hold this mutex for minutes and deafen the PO.
	// Never take startMu under mu.
	// jobs holds background work started by tools that must not block (🎯T600).
	jobs *jobRegistry

	startMu sync.Mutex
	// startFlights dedupes in-flight jevons_agent_start launches by name (🎯T792).
	startFlights startFlights
	// migrateOutcomes retains finished jevons_agent_migrate results (🎯T790).
	migrateOutcomes migrateOutcomes
	// launchAgentFn overrides registry.Launch (hermetic 🎯T541).
	launchAgentFn func(ctx context.Context, name string) (*claudia.Agent, error)
	// launchDeadline overrides defaultLaunchDeadline (🎯T541.2). Tests set a
	// short value so a hung Launch cannot sit for the product timeout.
	launchDeadline time.Duration

	// startStallGrace / startStallRetries override the 🎯T729 retry of an
	// opening brief whose CLI was still starting. Pointers so a test can
	// pin zero (retry immediately, or not at all) distinctly from unset.
	startStallGrace   *time.Duration
	startStallRetries *int
	// toolDeadline overrides tools/call bounds (🎯T254.5.1). Tests set a
	// short value so a hung handler cannot sit for the product timeout.
	toolDeadline time.Duration
	mcpFlightMu  sync.Mutex
	mcpFlightSeq uint64
	mcpFlights   map[string][]mcpFlight
	// mcpClients remembers each connection's initialize clientInfo so an
	// actor-less fleet-intent change names the client, not the overseer
	// (🎯T969, intent_actor.go).
	mcpClients mcpClientLedger
	// cursorSubmit / cursorBound are 🎯T541 seams. Bound means a live
	// process; Claudia owns whether the conversation is resumable.
	cursorSubmit func(name, text string) error
	cursorBound  func(name string) bool
	notifyJevon  NotifyFunc
	// overseerDeliver is the overseer arm of the single deliver-by-name path
	// (🎯T309.3). Wired from main to server.DeliverToOverseerAs so an
	// overseer-addressed send reuses the owner chat journal and notify queue.
	// Nil falls back to notifyJevon for agent-origin text (see deliverToOverseer).
	overseerDeliver OverseerDeliverFunc
	// notifyReplay remembers which notification batches the overseer already
	// holds, so the one channel every source funnels through refuses to
	// re-deliver byte-identical content (🎯T428). Nil until first use; see
	// notifyReplays(), which is the only reader of this field.
	notifyReplay *notifyReplayLedger
	// noticeCo coalesces materially unchanged fleet-health notices (🎯T810).
	noticeCo *noticeCoalescer

	// removals is the accounted-removal chokepoint (🎯T435), shared with the
	// HTTP server so a reap decided on that side is legible on the fleet
	// surfaces read on this one. Guarded by mu; nil until first use.
	removals *fleetlog.Account
	// resolveSender overrides fleet-agent process resolution on that same
	// path. Nil — the product path — resolves via the registry. Test seam.
	resolveSender senderResolver
	// agentSendLocks serialize admission to each provider seat. OMP's socket
	// write can succeed before the sidecar rejects an overlapping prompt.
	// Guarded by mu; each named lock is held only across that seat's send.
	agentSendLocks map[string]*sync.Mutex
	// unconfirmedSends remembers a delivered_unconfirmed verdict per seat so
	// stop / kill can refuse to act on an undecided delivery (🎯T664).
	// Guarded by mu.
	unconfirmedSends map[string]unconfirmedSend
	// oversizedSessions caches the per-session census of records over the
	// broker line limit, keyed by session path (🎯T661). Guarded by mu.
	oversizedSessions map[string]oversizedEntry
	// seatStopLedger records why each seat last stopped (🎯T662); massStopNotified
	// is the burst key already delivered to the overseer. Guarded by mu.
	seatStopLedger   *seatstop.Ledger
	massStopNotified string
	// resolveProc overrides which claudia process carries an agent's event
	// sink (🎯T426). Nil — the product path — reads the live registry.
	resolveProc agentProcResolver
	// observeTurnWitness overrides turn-evidence observation (🎯T387): what
	// the AGENT did after a send, as opposed to what the send call returned.
	// Nil — the product path — watches the live claudia process. Test seam.
	observeTurnWitness turnWitness
	// agentEventHook receives every fleet worker event (progress, assistant, …)
	// so the HTTP server can maintain RHS progress chrome (🎯T118).
	agentEventHook       func(name string, ev claudia.Event)
	agentRequestRecorder func(name, text string, origin SendOrigin) error
	costSnapshot         func() (*cost.Snapshot, error)
	// planUsage is GET /api/plan-usage as an overseer tool (🎯T390.1.4).
	planUsage func() planusage.Snapshot
	// planOverrides is the owner's band override per plan (🎯T948).
	planOverrides *planusage.OverrideStore

	// grokRun shells out to the Grok CLI for mid-session MCP reconnect (🎯T60).
	// Nil uses defaultGrokRun (exec of grok on PATH). Tests inject a fake.
	grokRun grokRunFunc

	// fleetBriefed tracks agents that already received FleetStandingBrief
	// on first jevons_agent_send (🎯T104 under fan-out).
	fleetBriefed map[string]bool
	// bounceRemint names seats whose session_id changed on this boot's
	// reattach (🎯T545.1). The post-restart wake still full_briefs them.
	// Fleet recover unstick does not treat the new id as the old turn.
	bounceRemint map[string]bool

	// envelopeChatter dedupes/rate-caps chatter-capped kinds (🎯T509).
	// Lazy; guarded by mu.
	envelopeChatter *envelope.Tracker

	// spawnFailureNotified remembers, per target/worker, the exact spawn
	// failure already surfaced to the product owner (🎯T433), so a leaf that
	// keeps failing on every 10-minute sweep produces one actionable notice
	// per distinct error rather than a drumbeat of identical ones. In-memory
	// on purpose: a daemon restart re-notifying once is the right side of
	// the trade. Guarded by mu; nil until first use.
	spawnFailureNotified map[string]string

	// agentTurnBegan tracks agents that have begun ≥1 confirmed turn in
	// this daemon process (start prompt or successful send — 🎯T305).
	// Distinct from registry Materialized (durable). Used so agent_list
	// can report never_briefed vs running for live zero-turn seats.
	agentTurnBegan map[string]bool

	// agentFlight tracks whether a turn is KNOWN to be running per agent
	// (🎯T416). Absent means unknown, which is a real answer and not a
	// default — see turn_flight.go. Guarded by mu.
	agentFlight map[string]TurnFlight

	// wedges holds what the daemon has seen of each in-flight turn since it
	// began: motion, a host handle lost under it, messages handed over, and
	// whether it has been called wedged (🎯T927). Its own leaf lock, so the
	// flight writers can feed it while holding mu.
	wedges turnWedges

	// wiredSinks records which process object currently carries this
	// daemon's event sink, per agent (🎯T426). Guarded by wireMu and NOT by
	// mu: the sink reaches into mu while claudia holds the agent lock, so
	// wiring under mu would close a lock cycle — see attachAgentSink.
	wireMu     sync.Mutex
	wiredSinks map[string]wiredSink

	// launching counts the launches currently in flight per agent (🎯T426).
	// A process that is registered and alive but not yet wired is a fault
	// only if nobody is in the middle of bringing it up; while a launch is
	// running it is simply a launch. Guarded by wireMu, with wiredSinks,
	// because the two are read together by the sweep.
	launching map[string]int
	// starting counts jevons_agent_start calls between registering a row and
	// returning (🎯T970): worktree setup, the wait on startMu and the launch.
	// Guarded by wireMu. launching alone covers only the launch hook.
	starting map[string]int

	// selfTestEnv builds the 🎯T110 pack environment (shared with HTTP).
	selfTestEnv SelfTestEnvFunc

	// workers tracks jwork lifecycle in SQLite + SSE (🎯T8.2). Nil = no-op.
	workers *workers.Tracker
	// doitEng is the execution-safety engine (🎯T8.3). Nil = spawn unguarded
	// by policy (tests without engine).
	doitEng *doit.Engine

	// agentSendQ is a per-agent FIFO of pending sends when the ACP session is
	// busy (🎯T115), durable across a daemon restart once SetSendQueueDir has
	// rooted it on disk (🎯T418). Nil until first use; see sendQueue().
	agentSendQ *sendq.Store
	// Terminal generations prevent a slow delivery witness from overwriting
	// an already-observed turn end or losing that end's queue-drain wakeup.
	agentTerminalGeneration map[string]uint64
	sendqAttemptNoticed     map[string]string

	// heldReapedNoticed remembers which held backlog the overseer has already
	// been told about, per reaped seat (🎯T582 / 🎯T706). The sweep is a timer;
	// without this the same finished seat produced a fleet-health alert every
	// thirty seconds. The value is the hold's head entry id rather than a bare
	// flag, so a seat reaped, restarted, drained and reaped again can still
	// announce its second, genuinely new backlog.
	heldReapedNoticed map[string]string

	// parentReportOffered is 🎯T731: report ids already offered to a parent,
	// keyed parent+"\x00"+reportID. Drain fulfills the first offer; a second
	// deliverByName of the same id is suppressed.
	parentReportOffered       map[string]struct{}
	parentReportOfferedLoaded bool

	// postReapWatches is 🎯T734: HEAD snapshots taken as a work seat is
	// reaped, so a leftover pane that keeps committing is attributed to
	// the parent instead of discovered by the next worker.
	postReapWatches map[string]postReapWatch

	// deadAgentStreak is the T717 generation for a dead-name set: stable
	// across consecutive sweeps of the same names (list-call echoes), new
	// when a name leaves the dead set and returns (a second death).
	deadAgentStreak map[string]uint64
	deadAgentGen    uint64

	// sweepNow is the backlog sweep's clock, injectable so a ten-minute
	// simulated run costs no wall time (🎯T582). Nil = time.Now.
	sweepNow func() time.Time

	// birthNow is the 🎯T679.2 birth-monitor clock. Nil = time.Now.
	birthNow func() time.Time

	// reportDeliveryNow is the 🎯T757 parent-routing age clock. Nil = time.Now.
	reportDeliveryNow func() time.Time
	// birthRoots overrides DefaultSessionRoots for hermetic existence lookups.
	birthRoots *discovery.Roots
	// seatAliveFn overrides process aliveness for hermetic list/sweep tests.
	seatAliveFn func(string) bool
	// birthStore is the durable accepted-prompt + notice-key ledger (🎯T679.2).
	birthStore *birthLedger

	// eventLogTail tails durable product logs (🎯T120). Nil = tool unregistered.
	eventLogTail EventLogTailFunc
	// eventLogger dual-writes server lifecycle events via HTTP Server.LogEvent
	// when wired from main (🎯T128.4). Nil = fall through to eventJournal/slog.
	eventLogger EventLoggerFunc
	// eventJournal is the durable product journal for MCP lifecycle dual-write
	// (🎯T128.1 / T128.4). Same file as GET /api/logs when SetEventJournal is wired.
	eventJournal *eventlog.Journal

	// migrator moves an existing agent between backends, carrying a
	// handover to the successor (🎯T285). Nil = jevons_agent_migrate
	// unregistered rather than half-working.
	migrator Migrator
	// Latest host execution result for each Claudia placement verdict. Read
	// alongside a fresh decision so a failed launch cannot look like a move.
	// planSweepMu serialises SweepPlanPolicy: the periodic tick and an
	// owner reauth can both start one, and two sweeps moving the same seat
	// race Claudia's migration (one sees the other's live handle).
	planSweepMu sync.Mutex
	// capacityWatch notices a plan becoming admissible again (🎯T977).
	capacityWatch planusage.CapacityWatch
	// capacityDeliver replaces the capacity notice delivery (tests).
	capacityDeliver func(name, text string) error
	// seatWait holds frontier targets whose spawn found no plan to seat on (🎯T980).
	seatWait seatWaits
	// midTurnAnswers relays a busy agent's mid-turn answer to a steered
	// question straight to its asker (🎯T902).
	midTurnOnce    sync.Once
	midTurnAnswers *midTurnAnswers
	// deliveryEscalation is 🎯T899's urgency profile (config).
	deliveryEscalation config.DeliveryEscalationConfig
	planDecisionMu     sync.RWMutex
	planLastResults    map[string]planusage.PlanAction

	// defaultProvider is the daemon-wide claudia backend for new agents
	// when agent_start / thread_spawn / jwork omit provider (🎯T148).
	// Empty means cli.ResolveProvider falls through to env / grok at use time.
	defaultProvider string
	mcp             mcpattach.Args

	// llmPortfolio is the multi-provider task-type routing seed (🎯T325.2).
	// Nil → cost.DefaultPortfolio(). Soft-cap overlays may come from budget.
	// 🎯T476: omit-provider mint follows config.yaml, not this table;
	// the seed remains for capacity soft caps and for naming a loser.
	llmPortfolio *cost.Portfolio
	// llmPortfolioFromFile is true when llmPortfolio came from
	// state_dir/llm-portfolio.json rather than the compiled seed.
	llmPortfolioFromFile bool
	// providerSoftCaps overlays portfolio soft caps (from budget.json).
	providerSoftCaps map[string]int
	// portfolioMu guards llmPortfolio / llmPortfolioFromFile /
	// providerSoftCaps, which the 🎯T574 watcher swaps at runtime.
	portfolioMu sync.RWMutex

	// rsiLoop is the residual phrase/eventlog mint path (🎯T92; opt-in product residual).
	// Nil until SetRSILoop; jevons_rsi_cycle requires it. Product path is rsiCoach (🎯T243).
	rsiLoop *rsi.Loop

	// rsiCoach posts judgments to the overseer; never files bullseye (🎯T243).
	// Nil until SetRSICoach.
	rsiCoach *rsi.Coach

	// staffOps holds cooldown state for one bounded ops cycle (🎯T325.4).
	// Nil until registerStaffOpsTools; pure policy in internal/staffops.
	staffOps *staffOpsState
	// sentinel holds cooldown/budget/grace state for durable 🎯T219 loop.
	// Nil until ensureSentinelRuntime / registerSentinelTools.
	sentinel *sentinelRuntime

	// secAuditor is the standing security interest (🎯T335). Nil until wired.
	secAuditor *secauditor.Interest
	// writExec confines high-risk fleet exec under writ (🎯T335).
	writExec      writconf.Executor
	writBin       string
	writAvailable bool

	// idleActivity tracks ACP phase for enter-idle detection (🎯T207).
	// Nil until StartIdleNudgeLoop; broadcastAgentEvent Observes transitions.
	idleActivity *IdleActivityTracker
	// idleNudgeLedger carries durable backoff/max for the 🎯T315 re-pressure
	// actuator and the post-restart resume sweep; may be nil (no StateDir).
	idleNudgeLedger *IdleNudgeLedger
	// idlePressureHooks are the optional 🎯T316/T317 collaborator seams for
	// the idle re-pressure actuator. Zero value = conservative defaults.
	idlePressureHooks IdlePressureHooks
	// impatience is the 🎯T317 ladder (T316 set + sinks). Nil = T315-only
	// SweepIdleNudges path. Installed from main via SetImpatienceEngine.
	impatience *ImpatienceEngine
	// idleEventLast debounces worker-idle events per agent name.
	idleEventLast map[string]time.Time
	// seatBlockerClears maps an agent to the content key of the blocked
	// finish-report an owner/parent message (or an explicit clear) lifted
	// (🎯T938). A newer stored report from the seat forgets the entry.
	seatBlockerClears map[string]string

	// ownerNotifier writes deterministic owner notices that depend on no
	// agent (🎯T415). Nil means exhaustion is logged but not reported,
	// which is the pre-T415 behaviour and is said loudly at the point of
	// use rather than discovered later.
	ownerNotifier OwnerNotifier
	// exhaustion dedups repeated convergence exhaustion per agent.
	exhaustion exhaustionState
	// recoverBin is the detached diagnostician (🎯T415.1); stateDir is
	// what it reads. Empty leaves diagnosis unavailable, which the
	// deterministic notice does not depend on.
	recoverBin string
	stateDir   string
	// spawnOrderEvidence caches the journalled start attempts 🎯T762 reconciles
	// against: the journal is a full scan, and /api/agents asks per row.
	spawnOrderMu       sync.Mutex
	spawnOrderEvidence spawnorder.Evidence
	spawnOrderReadAt   time.Time

	// intent is the 🎯T414 fleet-intent store: the deliberate answer to
	// "should this agent be running?", read by every control that spawns,
	// nudges, revives, repressures, or repairs. Nil resolves to all-working,
	// which is the pre-T414 behaviour. See fleet_intent.go.
	intent *fleetintent.Store

	// reapVerify is the 🎯T753 pending-verification ledger: reaped implementers
	// whose commits landed but whose target row stayed open.
	reapVerify *reapverify.Store

	// roleAssignments records agent-name → role (🎯T511 / 🎯T536.2). Nil
	// means role is derived only from purpose/name heuristics / AgentDef.Role.
	roleAssignments *roles.Assignments
	// roleCat resolves role definition files (builtin + overlays).
	roleCat roles.Catalog
	// pendingSpawnRole is set by handleAgentStart before stitchAgentStart so
	// hermetic callers of the 9-arg stitch keep compiling (role defaults inside).
	pendingSpawnRole string
	// pendingOwnerAsked is set with pendingSpawnRole: an ineligible
	// provider= pin sticks only when the owner named that dest (🎯T652).
	pendingOwnerAsked bool

	// autoSpawnPaused is config frontier_consume.disabled (🎯T407). The
	// sentinel reads this as daemon-held evidence the fleet cannot run —
	// ready leaves are then a pause, not a spawn gap. Guarded by mu.
	autoSpawnPaused bool

	// wakeBatch coalesces machine-generated events into one digest per
	// recipient (🎯T392.2). Debouncing above is per-worker and stops the
	// same worker firing twice; this is per-recipient and stops four
	// different workers each buying a full coordinator turn. Nil means
	// batching is off and every event delivers immediately.
	wakeBatch *wakebatch.Batcher

	// ideaStateDir roots the durable idea ledger (state_dir/ideas.json, 🎯T325.3).
	// Empty until SetIdeaStateDir; idea tools stay unregistered.
	ideaStateDir string

	// agentReportDir roots the durable agent-report store (🎯T388) so a
	// terminal report outlives the agent that wrote it. Empty until
	// SetAgentReportDir; jevons_agent_report_read stays unregistered and an
	// over-bound report is marked as cut without a retrieval handle.
	// Guarded by mu.
	agentReportDir string

	// sendApprovalNever names seats whose MCP jevons_agent_send is denied
	// the Grok "approval policy is never" way (🎯T690 hermetic). Production
	// mint does not populate this; parent-report is daemon-delivered
	// regardless. Guarded by mu.
	sendApprovalNever map[string]bool

	// research is the ambient research staff cycle (🎯T356): periodic context
	// refresh plus async feed triggers, writing durable versioned notes.
	// Nil until SetResearchAgent.
	research *research.Agent

	// auditor is the periodic full-scan audit cycle (🎯T357): bounded passes
	// over code, skills, and prompts on an advanced-tier model, folded into
	// durable residue. Nil until SetAuditor.
	auditor *audit.Auditor

	// capacityGov admits, defers, or degrades background work against the
	// remaining budget and concurrent load (🎯T359). Nil until
	// SetCapacityGovernor — ambient loops then run ungated, as before.
	capacityGov *capacity.Governor

	// paneList / paneKill are the 🎯T459 census I/O seams. Nil uses tmux
	// against the claudia fleet socket. Tests inject a fixture fleet.
	paneList func() ([]panecensus.Pane, error)
	paneKill func(id string) error

	// drainRestartAt tracks 🎯T530 restart-to-drain schedules after a parent
	// kill so fleet-health / live probes can apply RemintGraceWindow.
	// Guarded by mu.
	drainRestartAt map[string]time.Time

	// drainLaunch, when set, replaces registry.Launch during 🎯T530
	// restart-to-drain. Hermetic tests inject a stub that refuses without
	// spawning a provider; nil uses the real registry Launch.
	drainLaunch func(name string) (*claudia.Agent, error)

	// sendqPin / sendqPinFails track seats whose held sendq cannot be
	// delivered (🎯T599): the pin names the blocking message for
	// agent_list / fleet health, and the fail counts decide when repeated
	// delivery failure of one entry becomes a pin. Guarded by mu.
	sendqPin map[string]SendqPin
	// heldScannedAt is the transcript size at which an agent's held attempts
	// were last checked against it (reconcileHeldFromTranscript).
	heldScannedAt map[string]int64
	sendqPinFails map[string]sendqEntryFails
}

// Seats is the seat-state authority (🎯T766.2): the single answer to what
// is true about a seat, fed by the parties that know. Lazily built so a
// Server assembled by a test has one without ceremony.
func (s *Server) Seats() *seatstate.Authority {
	if s == nil {
		return nil
	}
	s.seatsOnce.Do(func() { s.seats = seatstate.New(seatstate.Args{}) })
	return s.seats
}

// SetSeats injects the daemon's one seat-state authority (🎯T766.2), built
// in main and shared with internal/server and internal/fleet. It must be
// called before anything asks Seats(); a private authority already built is
// a wiring error, logged rather than silently producing two answers.
func (s *Server) SetSeats(a *seatstate.Authority) {
	if s == nil || a == nil {
		return
	}
	injected := false
	s.seatsOnce.Do(func() {
		s.seats = a
		injected = true
	})
	if !injected {
		slog.Warn("seat authority already built before injection; wiring order is wrong",
			"component", "seatstate")
	}
}

// observeSeat folds what claudia says about one registry row into the
// authority (🎯T766.2).
//
// This is the feed the authority was missing: the event stream reports
// motion, but it is silent for a seat that is merely sitting there, and it
// is empty entirely whenever the sink is dark. claudia owns the process, so
// it is the only honest source for alive and in-flight.
//
// alive is passed in rather than re-read because the caller has already
// asked (seatAlive honours a test's override, which a second read here
// would bypass).
func (s *Server) observeSeat(d claudia.AgentDef, alive bool) {
	if s == nil || d.Name == "" {
		return
	}
	rep := seatstate.SeatReport{
		Name:     d.Name,
		Provider: string(d.Provider),
		Model:    d.Model,
		Alive:    alive,
		Known:    true,
	}
	if s.registry != nil {
		if proc := s.registry.Get(d.Name); proc != nil {
			rep.PromptInFlight = proc.PromptInFlight()
		} else if alive {
			// seatAlive says yes but the registry has no handle: we cannot
			// see the turn. Report identity and aliveness, and leave
			// in-flight to decay to unknown rather than asserting calm.
			rep.Known = false
			s.Seats().Observe(seatstate.Observation{
				Name: d.Name, Provider: string(d.Provider), Model: d.Model,
				Alive: seatstate.Yes, QueueDepth: seatstate.QueueUnknown,
				Source: "claudia.report", At: time.Now(),
			})
			return
		}
	}
	s.Seats().FromClaudia(rep, time.Now())
}

// seatInFlight is the one place that answers "is a turn running on this
// seat" (🎯T766.2, census derivation 5).
//
// It asks claudia, records the answer, and returns it. Asking rather than
// serving a cache is deliberate: the registry handle is right here, and a
// control about to act on a seat should not act on a two-minute-old reading
// when a current one costs nothing. The recording is what makes every other
// reader — the cockpit, a sweep, a supervisor — see the same answer.
//
// Unknown is returned when there is no handle to ask, and it is a real
// answer: callers must decide what to do about not knowing rather than
// receiving a false.
func (s *Server) seatInFlight(name string) seatstate.Tri {
	if s == nil || name == "" {
		return seatstate.Unknown
	}
	if s.registry != nil {
		if proc := s.registry.Get(name); proc != nil {
			inFlight := proc.PromptInFlight()
			s.Seats().FromClaudia(seatstate.SeatReport{
				Name: name, Alive: proc.Alive(), PromptInFlight: inFlight, Known: true,
			}, time.Now())
			return seatstate.TriOf(inFlight)
		}
	}
	if st, ok := s.Seats().Get(name); ok {
		return st.InFlight
	}
	return seatstate.Unknown
}

// observeRegistryLiveness feeds every registered seat's current liveness
// into the shared authority (🎯T766.2, census derivation 8: recoverDeadHandles).
// The sweep's own recovery decisions already read each seat's ProcState; this
// makes the authority hear the same answer even when nobody has listed
// agents recently, so a stale authority between agent_list calls is never
// the reason a dead-seat sweep and the cockpit disagree about a name.
func (s *Server) observeRegistryLiveness() {
	if s == nil || s.registry == nil {
		return
	}
	for _, d := range s.registry.List() {
		if d.Name == "" {
			continue
		}
		alive := s.seatAlive(d.Name)
		s.observeSeat(d, alive)
	}
}

// observeSessionPhase folds the transcript decoder's phase reading into the
// shared authority (🎯T766.2, census derivation 1/3: PhaseFromFile via
// classifyAgentSessionPhase / classifyAgentListPhase). This is the one place the
// 🎯T423 decoder's idle/working/unknown answer is recorded for everyone else
// to read, rather than staying local to whichever sweep happened to decode
// the transcript this tick.
//
// Only Phase is asserted. Alive/InFlight are left unclaimed here: the
// decoder read a transcript, not a process or an event stream, and it must
// not manufacture claims about signals it never looked at.
func (s *Server) observeSessionPhase(name string, phase turnev.Phase) {
	if s == nil || name == "" || phase == turnev.PhaseUnknown {
		return
	}
	s.Seats().Observe(seatstate.Observation{
		Name: name, Phase: phase,
		QueueDepth: seatstate.QueueUnknown,
		Source:     "transcript.fold", At: time.Now(),
	})
}

// SetDefaultProvider sets the daemon-wide claudia backend used when spawn
// tools omit provider (🎯T148). Pass the already-resolved default
// (cli.ResolveProvider("", cfg.Provider)); empty re-resolves from env at use.
func (s *Server) SetDefaultProvider(provider string) {
	s.defaultProvider = strings.TrimSpace(provider)
}

// SetMCP installs the live jevonsmcp attach used on every mint (claudia 🎯T40).
func (s *Server) SetMCP(a mcpattach.Args) {
	if s == nil {
		return
	}
	s.mcp = a
}

// NoteBounceRemint records seats whose session_id changed on this boot
// (🎯T545.1). Fleet recover unstick skips them. The post-restart wake
// still sends the full brief.
func (s *Server) NoteBounceRemint(names []string) {
	if s == nil || len(names) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bounceRemint == nil {
		s.bounceRemint = map[string]bool{}
	}
	for _, n := range names {
		if n != "" {
			s.bounceRemint[n] = true
		}
	}
}

func (s *Server) bounceReminted(name string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bounceRemint != nil && s.bounceRemint[name]
}

// SetLLMPortfolio installs the multi-provider routing seed (🎯T325.2).
// Nil clears to DefaultPortfolio at route time. Marks the seed as
// compiled (not a leftover file).
func (s *Server) SetLLMPortfolio(p *cost.Portfolio) {
	s.SetLLMPortfolioSource(p, false)
}

// SetLLMPortfolioSource installs the routing seed and records whether
// it came from state_dir/llm-portfolio.json (🎯T476 loser naming).
func (s *Server) SetLLMPortfolioSource(p *cost.Portfolio, fromFile bool) {
	if s == nil {
		return
	}
	s.portfolioMu.Lock()
	defer s.portfolioMu.Unlock()
	s.llmPortfolio = p
	s.llmPortfolioFromFile = fromFile && p != nil
}

// llmPortfolioSource reads the seed under portfolioMu (🎯T574: the watcher
// swaps it from its own goroutine).
func (s *Server) llmPortfolioSource() (*cost.Portfolio, bool) {
	if s == nil {
		return nil, false
	}
	s.portfolioMu.RLock()
	defer s.portfolioMu.RUnlock()
	return s.llmPortfolio, s.llmPortfolioFromFile
}

// SetProviderSoftCaps overlays session soft caps from budget.json
// provider_soft_caps (🎯T325.2). Nil/empty leaves compiled defaults.
func (s *Server) SetProviderSoftCaps(caps map[string]int) {
	if s == nil {
		return
	}
	s.portfolioMu.Lock()
	defer s.portfolioMu.Unlock()
	s.providerSoftCaps = caps
}

// effectivePortfolio returns the routing seed with soft-cap overlays applied.
func (s *Server) effectivePortfolio() *cost.Portfolio {
	base := cost.DefaultPortfolio()
	if s == nil {
		return base
	}
	s.portfolioMu.RLock()
	defer s.portfolioMu.RUnlock()
	if s.llmPortfolio != nil {
		base = s.llmPortfolio
	}
	if len(s.providerSoftCaps) > 0 {
		return base.MergeSoftCaps(s.providerSoftCaps)
	}
	return base
}

// harnessLoadCounts tallies registered agents by claudia provider id
// (session soft-cap input for portfolio routing — never USD).
func (s *Server) harnessLoadCounts() cost.LoadCounts {
	load := cost.LoadCounts{}
	if s == nil || s.registry == nil {
		return load
	}
	for _, d := range s.registry.List() {
		p := strings.ToLower(strings.TrimSpace(string(d.Provider)))
		if p == "" {
			p = string(cli.DefaultProvider)
		}
		load[p]++
	}
	return load
}

// resolvedDefaultProvider returns the effective default for new agents.
func (s *Server) resolvedDefaultProvider() claudia.Provider {
	// defaultProvider is the config-resolved value from main; pass as cfg
	// so env is only consulted when main left it empty.
	return cli.ResolveProvider("", s.defaultProvider)
}

// mintProviderPick is the 🎯T476 / 🎯T691 decision for stitchAgentStart.
// Explicit and resume stay fleet constraints. Omit-provider mint with a
// plan feed asks claudia.Resolve and records the pick — jevons does not
// re-adjudicate it. No feed → config default (T476).
//
// 🎯T475: omit-task_type derivation uses the agent name so product-owner
// seats (suffix -po) get ceo even when purpose=work — they must not
// inherit work→code_implement→Claude/Opus.
func (s *Server) mintProviderPick(providerArg, stored string, existed bool, taskTypeArg, purpose, name string, ownerAsked bool) cost.MintProviderPick {
	tt := cost.TaskTypeForMint(name, purpose, taskTypeArg)
	dec := s.effectivePortfolio().Route(tt, s.harnessLoadCounts())
	_, fromFile := s.llmPortfolioSource()
	cfg := string(s.resolvedDefaultProvider())
	var feedOK bool
	var cands []planusage.DestCand
	var now time.Time
	var th planusage.Thresholds
	var ok bool
	_, cands, now, th, ok = s.planPolicyInputs()
	if ok && len(cands) > 0 {
		feedOK = true
	}
	explicit := strings.ToLower(strings.TrimSpace(providerArg))
	// 🎯T652: a PO habit pin on a depleted dest is not a decision — drop
	// it so Claudia Resolve / the omit path can choose. Resume, an
	// eligible explicit, and owner_asked still win (🎯T148 / 🎯T583 tape 3).
	if explicit != "" && !existed && !ownerAsked && !s.providerDestEligible(explicit) {
		explicit = ""
	}
	// 🎯T791: an unsteerable pin is dropped the same way; the exclusion is
	// cited on the pick so the start result names it.
	droppedUnsteerable := ""
	if explicit != "" && !existed && !ownerAsked {
		if why := planusage.UnsteerableReason(explicit); why != "" {
			droppedUnsteerable = fmt.Sprintf("explicit %s dropped: unsteerable (%s)", explicit, why)
			explicit = ""
		}
	}
	// 🎯T948: a plan the owner has overridden into a dest band is where a
	// new seat goes. Claudia's Resolve classifies from the readings alone
	// and would send it elsewhere.
	if explicit == "" && !existed {
		explicit = s.planOverrideMint()
	}
	if explicit == "" && !existed && feedOK {
		resolved, err := planusage.ResolveMint(context.Background(), cands, now, th)
		if err != nil || resolved.Provider == "" {
			pick := cost.MintProviderPick{
				Provider:       "",
				Knob:           cost.KnobClaudia,
				LosingKnob:     cost.KnobConfig,
				LosingProvider: cfg,
				TaskType:       dec.TaskType,
			}
			if err != nil {
				pick.Detail = strings.TrimSpace(err.Error())
			}
			// 🎯T978: say which plan refused and why, not only that none matched.
			pick.Detail += ": " + planusage.MintRefusalDetail(cands, now, th)
			return pick
		}
		p := strings.ToLower(string(resolved.Provider))
		pick := cost.MintProviderPick{
			Provider: p,
			Knob:     cost.KnobClaudia,
			Detail:   strings.TrimSpace(resolved.Reason),
			TaskType: dec.TaskType,
		}
		if droppedUnsteerable != "" {
			pick.Detail += "; " + droppedUnsteerable
		}
		if cfg != "" && cfg != p {
			pick.LosingKnob = cost.KnobConfig
			pick.LosingProvider = cfg
		} else if fromFile {
			want := strings.ToLower(strings.TrimSpace(dec.Provider))
			if want != "" && want != p {
				pick.LosingKnob = cost.KnobPortfolioFile
				pick.LosingProvider = want
			}
		}
		return pick
	}
	return cost.PickMintProvider(cost.MintProviderArgs{
		ProviderArg:       explicit,
		Existed:           existed,
		StoredProvider:    stored,
		ConfigProvider:    cfg,
		Portfolio:         dec,
		PortfolioFromFile: fromFile,
		PlanFeedOK:        feedOK,
		OwnerAsked:        ownerAsked,
	})
}

func (s *Server) planPolicyInputs() (planusage.Snapshot, []planusage.DestCand, time.Time, planusage.Thresholds, bool) {
	th := planusage.DefaultThresholds()
	now := time.Now()
	if s == nil || s.planUsage == nil {
		return planusage.Snapshot{}, nil, now, th, false
	}
	snap := s.planUsage()
	if snap.Pending && len(snap.Backends) == 0 {
		return snap, nil, now, th, false
	}
	if !snap.At.IsZero() {
		now = snap.At
	}
	load := s.harnessLoadCounts()
	caps := s.EffectiveProviderSoftCaps()
	var cands []planusage.DestCand
	for _, be := range planusage.CockpitSnapshot(snap).Backends {
		p := strings.ToLower(strings.TrimSpace(be.Provider))
		cands = append(cands, planusage.DestCand{
			Provider: p, Backend: be, Load: load[p], Cap: caps[p],
		})
	}
	return snap, cands, now, th, true
}

// providerDestEligible reports whether harness may receive minted work.
// Unknown (no plan feed, or harness not in the snapshot) is true — same as
// cost.MintModelArgs.CodexEligible (🎯T390.1.5).
func (s *Server) providerDestEligible(harness string) bool {
	_, cands, now, th, ok := s.planPolicyInputs()
	if !ok {
		return true
	}
	want := strings.ToLower(strings.TrimSpace(harness))
	for _, c := range cands {
		p := strings.ToLower(strings.TrimSpace(c.Provider))
		if p == want {
			return planusage.DestEligible(c.Backend, now, th)
		}
	}
	return true
}

// New creates an MCP server providing the jevons tool surface. The durable
// thread model (butler) and jwork are the only worker lifecycles; the legacy
// manager-backed session tools were removed (🎯T41).
// transcript may be nil if transcript ops are not available.
func New(workerWD string, screenshot ScreenshotFunc, transcript *TranscriptOps) *Server {
	s := &Server{
		workerWD:   workerWD,
		screenshot: screenshot,
		transcript: transcript,
		bootAt:     time.Now(),
	}

	mcpSrv := server.NewMCPServer("jevons", "1.0.0", server.WithToolFilter(filterAndBudgetTools))
	s.mcpSrv = mcpSrv

	if s.screenshot != nil {
		s.addTool(
			mcp.NewTool("jevons_screenshot",
				mcp.WithDescription("Take a screenshot of the connected mobile client's current screen. Returns the file path of the saved PNG image."),
			),
			s.handleScreenshot,
		)
	}

	if s.transcript != nil {
		s.addTool(
			mcp.NewTool("jevons_transcript_read",
				mcp.WithDescription("Read a conversation transcript, one page at a time (🎯T942): by default the newest 40 turns, never more than about 16k tokens. The header says how many turns there are and how to ask for the previous page (before=<turn>). With agent=<name>, returns THAT agent's transcript only (registry session_id) — never substitutes the caller's/overseer transcript (🎯T304). Omit agent to read the active overseer/Jevon session. Returns turns with role and text."),
				mcp.WithString("agent", mcp.Description("Fleet agent name whose transcript to read (e.g. jv-t300-loading-earlier). Required for supervision of workers; omit for the active overseer session.")),
				mcp.WithNumber("limit", mcp.Description("Turns per page (default 40, at most 200). The page also stops at about 16k tokens.")),
				mcp.WithNumber("before", mcp.Description("Show the turns before this turn number (1-based), for the previous page. Omit for the newest turns.")),
			),
			s.handleTranscriptRead,
		)
		s.addTool(
			mcp.NewTool("jevons_transcript_rewind",
				mcp.WithDescription("Rewind the Jevon conversation to keep only the first N turns. A turn is a user message + assistant response. Set turns to 0 for a complete reset. The next message will start a fresh conversation."),
				mcp.WithNumber("turns", mcp.Required(), mcp.Description("Number of turns to keep (0 = reset)")),
			),
			s.handleTranscriptRewind,
		)
	}

	s.registerJobTools() // 🎯T600 handles for work that takes time
	s.registerJwork()
	s.registerMCPReconnect()
	s.registerAgentMigrate()
	s.registerStaffOpsTools()
	s.registerSentinelTools()
	s.registerWritSecurityTools()  // 🎯T335 security auditor + writ confinement
	s.registerGateShowTool()       // 🎯T697: supervisor gate lookup
	s.registerSendqReconcileTool() // 🎯T726: the legal move out of PINNED
	s.registerSpawnOrderTools()    // 🎯T762: which half of a spawn order was dropped

	s.transport = server.NewStreamableHTTPServer(mcpSrv, server.WithStateLess(true),
		server.WithHTTPContextFunc(toolProfileFromRequest))
	return s
}

// RegisterRoutes adds the MCP endpoint to the given mux. Requests are
// logged at debug level with the JSON-RPC method — MCP clients fail
// silently (a server that never connects just means "no tools"), so
// visibility here is the only way to diagnose tool-wiring gaps (🎯T50).
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("/mcp", s.mcpRequestLogger())
	// 🎯T406: owner-visible fleet intent / hard-block snapshot.
	mux.HandleFunc("GET /api/fleet-intent", s.handleFleetIntentHTTP)
	// 🎯T980: frontier targets waiting for a seat, for the play button.
	mux.HandleFunc("GET /api/seat-waits", s.handleSeatWaits)
}

// SetToolCallObserver is notified on every JSON-RPC tools/call this
// server handles (🎯T64.2). Nil is a no-op.
func (s *Server) SetToolCallObserver(fn func(name string, args map[string]any)) {
	if s == nil {
		return
	}
	s.toolCallObserver = fn
}

// ToolsListCount reports how many MCP tools/list requests have been
// served since boot — the boot-time oracle for "did any agent actually
// attach our tools?" (🎯T50: the Grok CLI drops servers silently).
func (s *Server) ToolsListCount() int64 { return atomic.LoadInt64(&s.toolsListCount) }

func (s *Server) mcpRequestLogger() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var method string
		var body []byte
		if r.Body != nil {
			var err error
			body, err = io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err == nil {
				r.Body = io.NopCloser(bytes.NewReader(body))
				var m struct {
					Method string `json:"method"`
				}
				_ = json.Unmarshal(body, &m)
				method = m.Method
			}
		}
		if method == "tools/list" {
			atomic.AddInt64(&s.toolsListCount, 1)
		}
		if method == "tools/call" {
			if name, args := parseToolsCall(body); name != "" && s.toolCallObserver != nil {
				s.toolCallObserver(name, args)
			}
		}
		slog.Debug("mcp request", "http", r.Method, "rpc", method, "ua", r.UserAgent())
		// 🎯T969: the transport builds each handler's context from the
		// request's, so the caller identity rides into the tool call.
		r = r.WithContext(withMCPOrigin(r.Context(), s.originOf(r, method, body)))
		s.transport.ServeHTTP(w, r)
	})
}

func parseToolsCall(body []byte) (name string, args map[string]any) {
	var req struct {
		Method string `json:"method"`
		Params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &req) != nil || req.Method != "tools/call" {
		return "", nil
	}
	name = strings.TrimSpace(req.Params.Name)
	if len(req.Params.Arguments) > 0 && string(req.Params.Arguments) != "null" {
		var m map[string]any
		if json.Unmarshal(req.Params.Arguments, &m) == nil && len(m) > 0 {
			args = m
		}
	}
	return name, args
}

// SetBudgetGuards wires the cost enforcer's AllowSpawn / AllowResume into
// every MCP worker-launch path. Call with enforcer methods when the cost
// guard is live; leave unset when the usage DB is unavailable.
func (s *Server) SetBudgetGuards(spawn func() error, resume func(id string, auto bool) error) {
	s.spawnGuard = spawn
	s.resumeGuard = resume
}

// SetWorkersTracker attaches the 🎯T8.2 worker observability store + SSE hub.
func (s *Server) SetWorkersTracker(t *workers.Tracker) {
	s.workers = t
}

// SetDoitEngine attaches the 🎯T8.3 execution-safety engine for jwork gating.
func (s *Server) SetDoitEngine(eng *doit.Engine) {
	s.doitEng = eng
}

// checkSpawnAllowed refuses new worker launch when the budget clamp has
// halted spawning. Returns an MCP tool-error result when blocked.
func (s *Server) checkSpawnAllowed() *mcp.CallToolResult {
	if s.spawnGuard == nil {
		return nil
	}
	if err := s.spawnGuard(); err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	return nil
}

// checkResumeAllowed refuses re-launch of a named worker when the budget
// clamp blocks resume (spawn-halt, throttle window, pause/kill clamp).
func (s *Server) checkResumeAllowed(id string) *mcp.CallToolResult {
	if s.resumeGuard == nil {
		return nil
	}
	if err := s.resumeGuard(id, false); err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	return nil
}

// --- tool handlers ---

func (s *Server) handleScreenshot(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := s.screenshot()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("screenshot failed: %v", err)), nil
	}
	return mcp.NewToolResultText(path), nil
}

// handleTranscriptRead returns turns for a named agent or the active overseer
// session. 🎯T304: agent=<name> must resolve that agent's registry session only;
// never fall back to GetID (caller/overseer) when a name is given — silent
// substitution makes supervisors treat another seat's words as the worker's.
func (s *Server) handleTranscriptRead(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.transcript == nil || s.transcript.Read == nil {
		return mcp.NewToolResultError("transcript reader unavailable"), nil
	}

	args := req.GetArguments()
	agentName := strings.TrimSpace(str(args["agent"]))

	var sessionID string
	switch {
	case agentName != "":
		if s.registry == nil {
			return mcp.NewToolResultError("agent registry unavailable"), nil
		}
		def := s.registry.Def(agentName)
		if def == nil {
			// Explicit not-found — do not substitute another agent's transcript.
			return mcp.NewToolResultError(fmt.Sprintf("agent %q not found", agentName)), nil
		}
		sessionID = strings.TrimSpace(def.SessionID)
		if sessionID == "" {
			// Explicit empty — no silent substitution of caller/overseer.
			return mcp.NewToolResultText(fmt.Sprintf(
				"agent %q has no session yet (empty transcript).", agentName)), nil
		}
	default:
		if s.transcript.GetID == nil {
			return mcp.NewToolResultText("No active Jevon session."), nil
		}
		sessionID = strings.TrimSpace(s.transcript.GetID())
		if sessionID == "" {
			return mcp.NewToolResultText("No active Jevon session."), nil
		}
	}

	var turns []map[string]any
	var err error
	if agentName != "" && s.transcript.ReadForSeat != nil {
		turns, err = s.transcript.ReadForSeat(agentName, sessionID)
	} else {
		turns, err = s.transcript.Read(sessionID)
	}
	if err != nil {
		if agentName != "" {
			// 🎯T597: never a bare not-found that reads as born-stuck. The
			// verdict names the paths searched, the restart context, and the
			// seat-activity evidence; an ACTIVE seat is a text result, not an
			// error, because error-shape is what pattern-matched to
			// "never begun a turn" in the 2026-08-31 incident (🎯T304 still
			// holds: no substitution of another session).
			msg, active := s.transcriptNotFoundVerdict(agentName, sessionID, err)
			if active {
				return mcp.NewToolResultText(msg), nil
			}
			return mcp.NewToolResultError(msg), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("read failed: %v", err)), nil
	}
	if len(turns) == 0 {
		if agentName != "" {
			return mcp.NewToolResultText(fmt.Sprintf(
				"agent %q transcript is empty.", agentName)), nil
		}
		return mcp.NewToolResultText("Transcript is empty."), nil
	}

	limit := transcriptPageTurns
	if n, ok := args["limit"].(float64); ok && n >= 1 {
		limit = min(int(n), transcriptMaxPageTurns)
	}
	end := len(turns) // exclusive
	if n, ok := args["before"].(float64); ok && n >= 1 {
		end = min(int(n)-1, len(turns))
	}
	// One page, newest first up to the limit and the byte budget, printed in
	// order (🎯T942): a whole transcript once put 673 KB into the overseer's
	// context in one call, twice in 18 minutes.
	var lines []string
	size := 0
	start := end
	for i := end - 1; i >= 0 && len(lines) < limit; i-- {
		role, _ := turns[i]["role"].(string)
		text, _ := turns[i]["text"].(string)
		line := fmt.Sprintf("Turn %d [%s]: %s\n", i+1, role, truncate(text, 200))
		if size+len(line) > transcriptPageBytes && len(lines) > 0 {
			break
		}
		lines = append(lines, line)
		size += len(line)
		start = i
	}
	var b strings.Builder
	if agentName != "" {
		fmt.Fprintf(&b, "agent=%s session=%s ", agentName, sessionDisplay(sessionID))
	}
	switch {
	case len(lines) == 0:
		fmt.Fprintf(&b, "turns=%d; nothing before turn %d.\n", len(turns), end+1)
	case start > 0:
		fmt.Fprintf(&b, "turns=%d showing %d-%d; %d earlier turns not shown: call again with before=%d for the previous page.\n",
			len(turns), start+1, end, start, start+1)
	default:
		fmt.Fprintf(&b, "turns=%d showing %d-%d.\n", len(turns), start+1, end)
	}
	for i := len(lines) - 1; i >= 0; i-- {
		b.WriteString(lines[i])
	}
	return mcp.NewToolResultText(b.String()), nil
}

// A transcript page (🎯T942): turns by default and at most, and the byte
// budget a page never passes (about 16k tokens).
const (
	transcriptPageTurns    = 40
	transcriptMaxPageTurns = 200
	transcriptPageBytes    = 64 << 10
)

func (s *Server) handleTranscriptRewind(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	turnsF, _ := args["turns"].(float64)
	keepTurns := int(turnsF)

	sessionID := s.transcript.GetID()
	if sessionID == "" {
		return mcp.NewToolResultText("No active session to rewind."), nil
	}

	if err := s.transcript.Truncate(sessionID, keepTurns); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("rewind failed: %v", err)), nil
	}

	if keepTurns == 0 {
		return mcp.NewToolResultText("Truncated session to zero turns. Restart the Jevon agent to begin a fresh conversation."), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Rewound to %d turns. The truncated context will be used on the next message.", keepTurns)), nil
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max] + "\n... (truncated)"
	}
	return s
}
