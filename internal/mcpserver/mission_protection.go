// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/missionbound"
)

// OpenMissionProtection installs the start bound before controls start. A
// malformed policy/history/state is a startup error, never a counter reset.
func (s *Server) OpenMissionProtection(dir string) error {
	policy := missionbound.DefaultPolicy()
	b, err := os.ReadFile(filepath.Join(dir, "mission-protection.json"))
	if err == nil {
		if err = json.Unmarshal(b, &policy); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	var history *os.File
	history, err = os.Open(eventlog.DefaultPath(dir))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if history != nil {
		defer history.Close()
	}
	path := filepath.Join(dir, "fleet", "mission-starts.json")
	var st *missionbound.Store
	if history == nil {
		st, err = missionbound.Open(path, policy, nil, nil, time.Now())
	} else {
		// Cache the git lookup: historical events often repeat the same directory.
		scopes := map[string]string{}
		st, err = missionbound.Open(path, policy, history, func(wd string) string {
			if v, ok := scopes[wd]; ok {
				return v
			}
			v := missionScope(wd)
			scopes[wd] = v
			return v
		}, time.Now())
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.missionStarts = st
	s.mu.Unlock()
	return nil
}

// Linked worktrees share the git common directory. A rename/new worktree may
// not create a fresh target budget. Non-git projects are scoped by directory.
func missionScope(workdir string) string {
	out, err := exec.Command("git", "-C", workdir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err == nil {
		workdir = strings.TrimSpace(string(out))
	}
	if abs, err := filepath.Abs(workdir); err == nil {
		workdir = abs
	}
	if real, err := filepath.EvalSymlinks(workdir); err == nil {
		workdir = real
	}
	return workdir
}

// reserveMissionStart is shared by manual/PO and unattended frontier starts.
// Neither force_engage nor a T753 reopen reaches the privileged override.
func (s *Server) reserveMissionStart(name, workdir, target, parent, purpose, actor string, override bool, reason string) (func(bool), error) {
	noop := func(bool) {}
	s.mu.Lock()
	st := s.missionStarts
	s.mu.Unlock()
	if st == nil {
		return noop, nil
	} // tests/embedders; production installs at startup
	if s.registry != nil {
		if d := s.registry.Def(name); d != nil {
			if target == "" {
				target = d.TargetID
			}
			if purpose == "" {
				purpose = d.Purpose
			}
		}
	}
	if DurableFleetAgent(name, purpose, s.isOverseerAgent) || target == "" {
		return noop, nil
	}
	id, notice, err := st.Reserve(missionbound.Start{Scope: missionScope(workdir), Target: normalizeAgentTargetID(target), Seat: name, Actor: actor, OverrideReason: reason}, override, actor == "owner" || s.isOverseerAgent(actor), time.Now())
	if notice != nil {
		fields := map[string]any{"name": name, "target_id": target, "metric": "target_starts", "count": notice.Count, "rank": notice.Rank, "population": notice.Population, "sigma": notice.Sigma}
		s.logLifecycle(compAgentLifecycle, "mission_anomaly", "warning", fields)
		if parent == "" {
			parent = s.overseerName()
		}
		if _, sendErr := s.deliverByName(parent, notice.String(), OriginAgent, false); sendErr != nil {
			slog.Warn("mission anomaly notice delivery failed", "parent", parent, "err", sendErr)
			s.notifyFleetHealth(name, notice.String()+" Parent delivery failed: "+sendErr.Error())
		}
	}
	if err != nil {
		return noop, err
	}
	if override {
		s.logLifecycle(compAgentLifecycle, "mission_start_override", "ok", map[string]any{"name": name, "target_id": target, "actor": actor, "reason": reason, "reservation": id})
	}
	return func(started bool) {
		if !started {
			if err := st.Cancel(id); err != nil {
				slog.Error("mission reservation cancellation failed; reservation retained", "name", name, "err", err)
			}
		}
	}, nil
}

func (s *Server) missionStartRefusal(name, target string, err error) string {
	s.logLifecycle(compAgentLifecycle, "start", "error", map[string]any{"name": name, "target_id": target, "err": "mission_start_bound", "detail": err.Error()})
	return fmt.Sprintf("T998 start refused: %v", err)
}
