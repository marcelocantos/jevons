// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package upgrade is coordinator-side scaffolding for 🎯T40: restart
// jevonsd without registry.StopAll(), persist agent handles for
// reattach (session_id + optional Grok connect-mode serve URL/PID).
//
// Conversation durability (session/load + agents.json) already survives
// a normal restart. Process durability uses claudia Grok connect-mode
// (detached `grok agent serve` + WebSocket) so agents outlive the
// coordinator when StopAll is skipped on upgrade exit.
package upgrade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// EnvUpgradeExit, when set to a truthy value ("1", "true", "yes") at the
// moment of SIGTERM/SIGINT, treats that shutdown as an upgrade exit
// (skip StopAll). SIGHUP always means upgrade exit.
const EnvUpgradeExit = "JEVONS_UPGRADE_EXIT"

// HandlesFileName is the snapshot written under StateDir on upgrade exit.
const HandlesFileName = "upgrade-handles.json"

// Mode is how the coordinator is leaving.
type Mode int

const (
	// ModeNormal stops agents on exit (historical behaviour).
	ModeNormal Mode = iota
	// ModeUpgrade leaves agent processes alone and records handles.
	ModeUpgrade
)

// StopAgents reports whether exit should call registry.StopAll().
func (m Mode) StopAgents() bool {
	return m != ModeUpgrade
}

// String is for logs.
func (m Mode) String() string {
	if m == ModeUpgrade {
		return "upgrade"
	}
	return "normal"
}

// EnvRequestsUpgrade is true when JEVONS_UPGRADE_EXIT is truthy.
func EnvRequestsUpgrade() bool {
	return truthy(os.Getenv(EnvUpgradeExit))
}

func truthy(v string) bool {
	switch v {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	default:
		return false
	}
}

// Handle is one live agent at upgrade exit. SessionID is the durable
// conversation key. ConnectURL + PID enable process reattach when Grok
// connect-mode was used (claudia detached serve).
type Handle struct {
	Name       string `json:"name"`
	SessionID  string `json:"session_id"`
	WorkDir    string `json:"workdir,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Alive      bool   `json:"alive"`
	PID        int    `json:"pid,omitempty"`
	ConnectURL string `json:"connect_url,omitempty"`
	// TmuxWindowID is the Claude session window left running on upgrade
	// exit (e.g. "@3"). Empty when the agent was never tmux-backed or
	// had already exited.
	TmuxWindowID string `json:"tmux_window_id,omitempty"`
}

// Snapshot is the on-disk upgrade handoff for the next coordinator.
type Snapshot struct {
	// WrittenAt is RFC3339 when the old process exited for upgrade.
	WrittenAt string `json:"written_at"`
	// CoordinatorPID is the exiting jevonsd PID (audit only).
	CoordinatorPID int `json:"coordinator_pid"`
	// Residual documents why process reattach is incomplete (empty when ready).
	Residual string `json:"residual,omitempty"`
	// Agents are live (or registered) handles at exit.
	Agents []Handle `json:"agents"`
}

// ResidualConnectMode is the standing gap when no connect-mode endpoints
// were externalized (stdio-only Grok or agents never launched durable).
const ResidualConnectMode = "no process-reattach handles in handoff: Claude needs a live tmux window id, Grok needs CLAUDIA_GROK_CONNECT (detached serve + WebSocket); conversation reattach by session_id still works"

// SnapshotPath returns StateDir/upgrade-handles.json.
func SnapshotPath(stateDir string) string {
	return filepath.Join(stateDir, HandlesFileName)
}

// BuildSnapshot assembles a handoff from registry-shaped inputs.
// Residual is empty when at least one agent has connect-mode URL+PID.
func BuildSnapshot(agents []Handle, coordinatorPID int) Snapshot {
	residual := ResidualConnectMode
	for _, a := range agents {
		if processReattachable(a) {
			residual = ""
			break
		}
	}
	return Snapshot{
		WrittenAt:      time.Now().UTC().Format(time.RFC3339),
		CoordinatorPID: coordinatorPID,
		Residual:       residual,
		Agents:         agents,
	}
}

// SaveSnapshot writes snap atomically under path (write temp + rename).
func SaveSnapshot(path string, snap Snapshot) error {
	if path == "" {
		return fmt.Errorf("upgrade snapshot: empty path")
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("upgrade snapshot marshal: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("upgrade snapshot dir: %w", err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("upgrade snapshot write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("upgrade snapshot rename: %w", err)
	}
	return nil
}

// ConsumeSnapshot removes a handoff after the successor has acted on
// it, so a later drain boot is not mistaken for an upgrade.
func ConsumeSnapshot(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("upgrade snapshot remove: %w", err)
	}
	return nil
}

// LoadSnapshot reads a previous upgrade handoff. Missing file returns
// (nil, nil). Malformed file is a hard error (no silent reset).
func LoadSnapshot(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("upgrade snapshot read: %w", err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("upgrade snapshot parse %s: %w", path, err)
	}
	return &snap, nil
}

// SessionIDs returns non-empty session ids from a snapshot (reattach plan).
func SessionIDs(snap *Snapshot) []string {
	if snap == nil {
		return nil
	}
	out := make([]string, 0, len(snap.Agents))
	for _, a := range snap.Agents {
		if a.SessionID != "" {
			out = append(out, a.SessionID)
		}
	}
	return out
}
