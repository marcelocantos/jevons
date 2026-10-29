// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/server"
)

// startControlServices is the daemon startup composition. The shared
// control-plane link and receipt readers exist independently of optional
// accounting, including when usage.db cannot open. The shared link opens
// only Jevons' own replica and publishes no lifecycle intents. Accounting
// retains its existing startup effects and predicates.
// The caller owns Close on the returned replica, even when guard is nil.
func startControlServices(ctx context.Context, cfg config.Config, watcher *config.Watcher, registry *claudia.Registry, scanner *discovery.Scanner, srv *server.Server, mcp *mcpserver.Server) (*cost.ReplicaSource, *costGuard) {
	replica, err := cost.OpenReplica(ctx, cfg.StateDir)
	if err != nil {
		slog.Error("control plane unavailable — receipt replication disabled", "err", err)
	} else {
		if mcp != nil {
			mcp.SetSendReceiptQuery(replica.ReplicatedReceipt)
		}
		if srv != nil {
			srv.SetSendReceiptQuery(replica.ReplicatedReceipt)
		}
	}
	guard := startCostGuard(ctx, cfg, watcher, registry, scanner, srv, mcp, replica)
	return replica, guard
}
