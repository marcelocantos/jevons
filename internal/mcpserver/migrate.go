// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/thread"
)

// Migrator is the fleet capability behind jevons_agent_migrate (🎯T285):
// rotate an agent onto a new backend, then hand its successor the
// predecessor's transcript. Implemented by *fleet.Claudia; an optional
// dependency so the tool is simply unavailable when it is not wired,
// rather than half-working.
type Migrator interface {
	PrepareMigration(name string, to claudia.Provider, force bool) (handover.Pending, error)
	SeedSuccessor(name string) (handover.Pending, bool, error)
	Launch(t *thread.Thread) error
}

// SetMigrator wires the provider-migration capability.
func (s *Server) SetMigrator(m Migrator) { s.migrator = m }

func (s *Server) registerAgentMigrate() {
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_agent_migrate",
			mcp.WithDescription(
				"Move an existing agent to a different backend (claudia provider id: grok, claude, …) "+
					"WITHOUT losing what it was doing. Sessions cannot cross backends, so the agent is "+
					"rotated onto a fresh session and its successor is handed the path to the "+
					"predecessor's transcript with instructions to read it and pick up where it left "+
					"off (🎯T285). The owner-visible chat log is untouched. Refuses when the "+
					"predecessor's transcript cannot be found — pass force=true to switch cold on "+
					"purpose. Use this instead of stopping and re-creating an agent, which starts it "+
					"with no history at all."),
			mcp.WithString("name", mcp.Required(),
				mcp.Description("Agent to migrate (registry name, e.g. jevons-po)")),
			mcp.WithString("provider", mcp.Required(),
				mcp.Description("Target backend: claudia provider id (grok, claude, …)")),
			mcp.WithBoolean("force",
				mcp.Description("Switch even when no predecessor transcript is found — a deliberate cold start")),
		),
		s.handleAgentMigrate,
	)
}

func (s *Server) handleAgentMigrate(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.migrator == nil {
		return mcp.NewToolResultError("agent migration is not available (no fleet migrator wired)"), nil
	}
	args := req.GetArguments()
	name := strings.TrimSpace(fmt.Sprint(args["name"]))
	target := strings.TrimSpace(fmt.Sprint(args["provider"]))
	force, _ := args["force"].(bool)
	if name == "" || target == "" {
		return mcp.NewToolResultError("name and provider are required"), nil
	}

	// The overseer is attached to owner chat by the HTTP server, not by the
	// registry, so its migration needs the server-side re-attach that this
	// tool cannot perform. It is also the session answering this very call:
	// rotating it from here would kill the conversation mid-turn. Both
	// reasons point at the same place — the owner-driven endpoint.
	if s.registry != nil {
		if def := s.registry.Def(name); def != nil && def.Purpose == claudia.PurposeOverseer {
			return mcp.NewToolResultError(fmt.Sprintf(
				"%q is the overseer: migrating it also has to re-attach owner chat, and it is the "+
					"session answering this call — rotating it from here would end the turn mid-flight. "+
					"The owner drives it instead: POST /api/overseer/migrate {\"provider\":\"…\"} "+
					"(🎯T285). Fleet agents migrate normally through this tool.", name)), nil
		}
	}

	pending, err := s.migrator.PrepareMigration(name, claudia.Provider(target), force)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// The row is already rotated, so a launch failure must not read as
	// "nothing happened": the handover is on disk and the next successful
	// launch delivers it.
	if err := s.migrator.Launch(&thread.Thread{ID: name}); err != nil {
		slog.Error("migration rotated but launch failed; handover stays pending",
			"name", name, "err", err)
		return mcp.NewToolResultError(fmt.Sprintf(
			"%s is now registered on %s but did not start: %v\n"+
				"Its handover is saved — start it again and the successor is handed %s.",
			name, pending.To, err, pending.TranscriptPath)), nil
	}

	seeded, ok, serr := s.migrator.SeedSuccessor(name)
	if serr != nil {
		return mcp.NewToolResultError(fmt.Sprintf(
			"%s started on %s but the handover was not delivered: %v (it stays pending)",
			name, pending.To, serr)), nil
	}
	if !ok {
		return mcp.NewToolResultText(fmt.Sprintf(
			"%s migrated to %s COLD — no predecessor transcript was handed over.",
			name, pending.To)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf(
		"%s migrated %s → %s. Successor is reading %s and will continue from there.\n%s",
		name, seeded.From, seeded.To, seeded.TranscriptPath, seeded.Describe())), nil
}
