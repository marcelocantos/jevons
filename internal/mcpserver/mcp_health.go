// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/mcphealth"
	"github.com/marcelocantos/jevons/internal/seatprovider"
)

// 🎯T379: an agent inherits its MCP server list from the provider's
// user-scoped config, and a registration pointing at a port nothing serves
// does not fail — the client sits in "still connecting" and the agent runs
// without those tools. Nobody notices, because nothing looks.
//
// Agent start is the right place to look: it is the moment the inherited
// list becomes that agent's tool surface, and the caller who spawned the
// agent is the party who can act on a fault.

// mcpHealthTTL caches a sweep briefly so a fan-out of spawns probes once
// rather than once per child. Short enough that a registration fixed
// mid-session stops being reported almost immediately.
const mcpHealthTTL = 30 * time.Second

// mcpHealthCache memoizes the last sweep per provider.
type mcpHealthCache struct {
	mu   sync.Mutex
	at   time.Time
	prov seatprovider.Identity
	note string
}

var mcpHealth mcpHealthCache

// claudeUserConfigPath is where Claude Code keeps the user-scope server map
// that every agent inherits, regardless of workdir.
func claudeUserConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude.json")
}

// mcpRegistrationsFor reads the registrations an agent of this provider will
// inherit at start.
//
// A Claude Code CLI seat only: that CLI inherited ~/.claude.json at start,
// the seat that also keeps a Claude JSONL (providerKeepsClaudeTranscript).
// A sidecar seat is handed AgentDef.MCPServers instead, and its launch names
// every server that listed no tools (🎯T934); a dead registration never
// fails that launch. A row storing the plan name claude is the anthropic
// sidecar seat it launches as (🎯T1079), which never had this note, so the
// two spellings agree. Grok kept its user-scope list in ~/.grok/config.toml,
// and parsing TOML would mean taking a dependency this project declines.
// Declared residual rather than a silent gap: ok=false says so instead of
// reporting "all healthy".
func mcpRegistrationsFor(provider claudia.Provider) (regs []mcphealth.Registration, ok bool) {
	if !providerKeepsClaudeTranscript(provider) {
		return nil, false
	}
	path := claudeUserConfigPath()
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Debug("mcp health: cannot read claude user config", "path", path, "err", err)
		return nil, false
	}
	regs, err = mcphealth.ParseClaudeRegistrations(data)
	if err != nil {
		slog.Debug("mcp health: cannot parse claude user config", "path", path, "err", err)
		return nil, false
	}
	return regs, true
}

// mcpHealthNote probes the MCP registrations an agent of this provider
// inherits and returns a one-line warning naming any that provably cannot
// resolve, or "" when there is nothing to say. Never returns an error: a
// health check that can block a spawn would be worse than the fault it
// looks for.
func mcpHealthNote(provider claudia.Provider) string {
	mcpHealth.mu.Lock()
	if mcpHealth.prov == seatprovider.Resolve(provider) && time.Since(mcpHealth.at) < mcpHealthTTL {
		note := mcpHealth.note
		mcpHealth.mu.Unlock()
		return note
	}
	mcpHealth.mu.Unlock()

	note := ""
	if regs, ok := mcpRegistrationsFor(provider); ok {
		note = mcphealth.Report(mcphealth.ProbeAll(regs, mcphealth.DefaultTimeout))
	}

	mcpHealth.mu.Lock()
	mcpHealth.prov, mcpHealth.at, mcpHealth.note = seatprovider.Resolve(provider), time.Now(), note
	mcpHealth.mu.Unlock()
	return note
}

// noteAgentMCPHealth surfaces a dead-registration fault for a just-started
// agent on all three channels the target allows: the eventlog (durable), the
// overseer (a human-visible note), and the agent_start result the spawning
// caller reads. Returns the suffix for that result, or "" when healthy.
//
// The agent is already running by this point and is left running: missing
// tools degrade a mission, they do not invalidate it, and refusing the spawn
// would turn an observability fix into an outage.
func (s *Server) noteAgentMCPHealth(name string, provider claudia.Provider) string {
	note := mcpHealthNote(provider)
	if note == "" {
		return ""
	}
	s.logLifecycle(compAgentLifecycle, "mcp_health", "warn", map[string]any{
		"name": name, "provider": string(provider), "detail": note,
	})
	line := fmt.Sprintf("%s started with a %s", name, note)
	sid := name
	if s.registry != nil {
		if def := s.registry.Def(name); def != nil {
			if x := strings.TrimSpace(def.SessionID); x != "" {
				sid = x
			}
		}
	}
	occ := fleetHealthEventOccurrence(sid, time.Now())
	line = mixFleetHealthOccurrence(line, occ)
	if line == "" {
		return " — WARNING: " + note
	}
	if _, err := s.deliverByName(s.overseerName(), "[MCP health] "+line, OriginAgent, false); err != nil {
		slog.Debug("mcp health note undelivered", "err", err)
	}
	slog.Warn("agent started against a dead MCP registration",
		"name", name, "provider", provider, "detail", note)
	return " — WARNING: " + note
}

// mcpHealthResetForTest clears the memoized sweep so a test can observe a
// fresh probe.
func mcpHealthResetForTest() {
	mcpHealth.mu.Lock()
	mcpHealth.prov, mcpHealth.at, mcpHealth.note = seatprovider.Identity{}, time.Time{}, ""
	mcpHealth.mu.Unlock()
}

// mcpHealthNoteFromRegs is the seam a hermetic test uses to drive the
// surfacing path from fixture registrations without touching the real
// user config.
func mcpHealthNoteFromRegs(regs []mcphealth.Registration) string {
	return strings.TrimSpace(mcphealth.Report(mcphealth.ProbeAll(regs, mcphealth.DefaultTimeout)))
}
