// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/panecensus"
)

const (
	compPaneCensus = "pane_census"
	tmuxListFormat = "#{session_name}\t#{window_name}\t#{pane_id}\t#{pane_pid}\t#{pane_title}\t#{@claudia-agent-name}\t#{@claudia-session-id}"
)

// SetPaneCensusIO overrides tmux list/kill for hermetic tests (🎯T459).
func (s *Server) SetPaneCensusIO(list func() ([]panecensus.Pane, error), kill func(id string) error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paneList = list
	s.paneKill = kill
}

// SweepOrphanPanes reaps fleet tmux panes the registry does not know about
// and that are not mid-turn (🎯T459). Every reap is named in the eventlog.
// Returns how many panes were killed.
func (s *Server) SweepOrphanPanes() int {
	if s == nil {
		return 0
	}
	list, kill := s.paneIO()
	panes, err := list()
	if err != nil {
		slog.Debug("pane census: list failed", "err", err)
		return 0
	}
	names := s.registryNames()
	s.annotateFlight(panes)
	r := panecensus.Plan(panes, names, panecensus.DefaultWarmPoolMax)
	killed := 0
	for _, d := range r.Reap() {
		id := strings.TrimSpace(d.Pane.ID)
		if id == "" {
			continue
		}
		if err := kill(id); err != nil {
			slog.Warn("pane census: kill failed", "pane", id, "name", d.Pane.Name(), "err", err)
			s.LogEvent(compPaneCensus, "reap_error", map[string]any{
				"pane": id, "name": d.Pane.Name(), "reason": d.Reason, "err": err.Error(),
			})
			continue
		}
		killed++
		slog.Info("pane census: reaped", "pane", id, "name", d.Pane.Name(), "reason", d.Reason)
		s.LogEvent(compPaneCensus, "reap", map[string]any{
			"pane": id, "name": d.Pane.Name(), "window": d.Pane.Window,
			"session": d.Pane.Session, "reason": d.Reason,
		})
	}
	return killed
}

// FormatHostCostLines is the agent_list footer: estimated per-agent cost
// plus the last census of panes vs registry (🎯T459 §2).
func (s *Server) FormatHostCostLines(registered int) string {
	cost := panecensus.EstimateCost(registered)
	lines := panecensus.FormatCost(cost)
	list, _ := s.paneIO()
	panes, err := list()
	if err != nil || len(panes) == 0 {
		return lines
	}
	s.annotateFlight(panes)
	r := panecensus.Plan(panes, s.registryNames(), panecensus.DefaultWarmPoolMax)
	return lines + "\n" + panecensus.FormatCensus(r, panecensus.DefaultWarmPoolMax)
}

func (s *Server) paneIO() (func() ([]panecensus.Pane, error), func(id string) error) {
	s.mu.Lock()
	list, kill := s.paneList, s.paneKill
	s.mu.Unlock()
	if list == nil {
		list = defaultListFleetPanes
	}
	if kill == nil {
		kill = defaultKillFleetPane
	}
	return list, kill
}

func (s *Server) registryNames() map[string]bool {
	out := map[string]bool{}
	if s == nil || s.registry == nil {
		return out
	}
	for _, d := range s.registry.List() {
		if n := strings.TrimSpace(d.Name); n != "" {
			out[n] = true
		}
		if id := strings.TrimSpace(d.SessionID); id != "" {
			out[id] = true
		}
	}
	return out
}

func (s *Server) annotateFlight(panes []panecensus.Pane) {
	if s == nil {
		return
	}
	for i := range panes {
		name := panes[i].Name()
		switch s.flightState(name) {
		case FlightInFlight:
			panes[i] = panes[i].WithFlight(panecensus.FlightInFlight)
		case FlightIdle:
			panes[i] = panes[i].WithFlight(panecensus.FlightIdle)
		default:
			// Unknown: leave title inference in place so an empty-prompt
			// orphan still classifies as idle.
		}
	}
}

func defaultListFleetPanes() ([]panecensus.Pane, error) {
	sock := cli.AgentTmuxSocket()
	if strings.TrimSpace(sock) == "" {
		return nil, fmt.Errorf("pane census: empty tmux socket")
	}
	out, err := exec.Command("tmux", "-S", sock, "list-panes", "-a", "-F", tmuxListFormat).Output()
	if err != nil {
		return nil, err
	}
	return panecensus.ParseListPanes(string(out)), nil
}

func defaultKillFleetPane(id string) error {
	sock := cli.AgentTmuxSocket()
	if strings.TrimSpace(sock) == "" || strings.TrimSpace(id) == "" {
		return fmt.Errorf("pane census: empty socket or pane id")
	}
	return exec.Command("tmux", "-S", sock, "kill-pane", "-t", id).Run()
}
