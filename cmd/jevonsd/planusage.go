// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"

	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/server"
)

// startPlanUsage wires the subscription plan-usage reader (🎯T390): how much
// of each backend's allowance is left and when it rolls over, polled from
// claudia and served at GET /api/plan-usage for the cockpit.
//
// claudia 🎯T18 built the producer and nothing ever called it, so the owner
// ran dozens of agents with no view of the allowance they were spending. This
// is the consumer. It is deliberately unconditional — it does not hang off the
// cost guard, because the cost guard is switched off (budget.json
// disabled=true, owner, 2026-08-03) and plan remaining is exactly the number
// that stays meaningful when dollars do not.
func startPlanUsage(ctx context.Context, mcpSrv *mcpserver.Server, srv *server.Server) *planusage.Reader {
	reader := planusage.NewReader(planusage.ReaderArgs{
		Load: mcpSrv.HarnessLoad,
	})
	srv.SetPlanUsageSource(func() any { return reader.Snapshot() })
	go reader.Run(ctx)

	slog.Info("plan usage ready (🎯T390)",
		"api", "GET /api/plan-usage",
		"refresh", planusage.DefaultRefresh,
		"stale_after", planusage.DefaultStaleAfter,
		"grok_opt_in_env", planusage.GrokUsageEnv,
	)
	return reader
}
