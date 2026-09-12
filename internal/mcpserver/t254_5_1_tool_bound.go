// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"
)

// 🎯T254.5.1 — every jevons_* tools/call runs under a deadline, and
// interrupt/session-cancel cancels that context so a hung handler cannot
// hold a spawn-class turn hostage.

const (
	defaultSpawnClassToolDeadline = 30 * time.Second
	defaultWorkerToolDeadline     = 10 * time.Minute
)

type mcpFlight struct {
	id     uint64
	cancel context.CancelFunc
}

func (s *Server) addTool(t mcp.Tool, h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	if s == nil || s.mcpSrv == nil {
		return
	}
	s.mcpSrv.AddTool(t, s.boundTool(t.Name, h))
}

func (s *Server) boundTool(name string, h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if ctx == nil {
			ctx = context.Background()
		}
		wait := s.toolCallWait(name)
		ctx, cancel := context.WithTimeout(ctx, wait)
		defer cancel()
		caller := s.mcpCallerOf(req)
		id := s.trackMCPFlight(caller, cancel)
		defer s.untrackMCPFlight(caller, id)

		type outcome struct {
			res *mcp.CallToolResult
			err error
		}
		ch := make(chan outcome, 1)
		go func() {
			res, err := h(ctx, req)
			ch <- outcome{res, err}
		}()
		select {
		case out := <-ch:
			return out.res, out.err
		case <-ctx.Done():
			return mcp.NewToolResultError(fmt.Sprintf(
				"tools/call %s timed out or cancelled after %s (🎯T254.5.1)", name, wait)), nil
		}
	}
}

func (s *Server) toolCallWait(name string) time.Duration {
	if s != nil && s.toolDeadline > 0 {
		return s.toolDeadline
	}
	if spawnClassTool(name) {
		return defaultSpawnClassToolDeadline
	}
	return defaultWorkerToolDeadline
}

func spawnClassTool(name string) bool {
	n := strings.TrimSpace(name)
	if strings.HasPrefix(n, "jevons_agent_") || strings.HasPrefix(n, "jevons_fleet") ||
		strings.HasPrefix(n, "jevons_thread_") || n == "jevons_jwork" || n == "jevons_capacity" {
		return true
	}
	return false
}

func (s *Server) mcpCallerOf(req mcp.CallToolRequest) string {
	args := req.GetArguments()
	if v, ok := args["actor"].(string); ok {
		if a := strings.TrimSpace(v); a != "" {
			return a
		}
	}
	if s == nil || s.registry == nil {
		return ""
	}
	var inflight []string
	for _, d := range s.registry.List() {
		if d.Name == "" {
			continue
		}
		if proc := s.registry.Get(d.Name); proc != nil && proc.PromptInFlight() {
			inflight = append(inflight, d.Name)
		}
	}
	if len(inflight) == 1 {
		return inflight[0]
	}
	return ""
}

func (s *Server) trackMCPFlight(caller string, cancel context.CancelFunc) uint64 {
	if s == nil || cancel == nil {
		return 0
	}
	s.mcpFlightMu.Lock()
	defer s.mcpFlightMu.Unlock()
	s.mcpFlightSeq++
	id := s.mcpFlightSeq
	if s.mcpFlights == nil {
		s.mcpFlights = map[string][]mcpFlight{}
	}
	s.mcpFlights[caller] = append(s.mcpFlights[caller], mcpFlight{id: id, cancel: cancel})
	return id
}

func (s *Server) untrackMCPFlight(caller string, id uint64) {
	if s == nil || id == 0 {
		return
	}
	s.mcpFlightMu.Lock()
	defer s.mcpFlightMu.Unlock()
	list := s.mcpFlights[caller]
	kept := list[:0]
	for _, f := range list {
		if f.id == id {
			continue
		}
		kept = append(kept, f)
	}
	if len(kept) == 0 {
		delete(s.mcpFlights, caller)
		return
	}
	s.mcpFlights[caller] = kept
}

// cancelMCPFlights cancels in-process tools/call handlers for name
// (and unnamed flights when this seat is the only plausible caller).
func (s *Server) cancelMCPFlights(name string) {
	if s == nil {
		return
	}
	name = strings.TrimSpace(name)
	s.mcpFlightMu.Lock()
	defer s.mcpFlightMu.Unlock()
	run := func(key string) {
		for _, f := range s.mcpFlights[key] {
			if f.cancel != nil {
				f.cancel()
			}
		}
		delete(s.mcpFlights, key)
	}
	if name != "" {
		run(name)
	}
	if name != "" && len(s.mcpFlights[""]) > 0 {
		run("")
	}
}

// isSpawnClassSeat reports PO / overseer seats whose hung MCP is a
// control-plane outage (🎯T254.5.2).
func isSpawnClassSeat(name, purpose string) bool {
	if purpose == claudia.PurposeOverseer {
		return true
	}
	return isPOName(name)
}
