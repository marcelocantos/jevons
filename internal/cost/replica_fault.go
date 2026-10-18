// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build jevons_claudia_next

package cost

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

// FaultReplicaFiles disturbs one shared-plane handshake on the files under
// stateDir. drop discards the Claudia peer's opening messages and returns
// how many were discarded. duplicate runs two full SyncLocked handshakes;
// the second replays queued changesets, and the return is 1.
// CatchUpReplicas does not call this. A later catch-up opens a new
// handshake, which is how a lost frame is recovered and a replayed frame
// stays one usage row.
func FaultReplicaFiles(stateDir string, drop, duplicate bool) (int, error) {
	if drop == duplicate {
		return 0, fmt.Errorf("control plane: fault requires exactly one of drop or duplicate")
	}
	if strings.TrimSpace(stateDir) == "" {
		return 0, fmt.Errorf("control plane: fault state dir")
	}
	claudiaDB, err := sqlpipe.OpenDatabase(filepath.Join(stateDir, controlplane.ClaudiaDBFile))
	if err != nil {
		return 0, err
	}
	defer claudiaDB.Close()
	jevonsDB, err := sqlpipe.OpenDatabase(filepath.Join(stateDir, controlplane.JevonsDBFile))
	if err != nil {
		return 0, err
	}
	defer jevonsDB.Close()
	if err := controlplane.EnsureSchema(claudiaDB); err != nil {
		return 0, err
	}
	if err := controlplane.EnsureSchema(jevonsDB); err != nil {
		return 0, err
	}
	if drop {
		return dropReplicaHandshake(claudiaDB, jevonsDB)
	}
	if err := controlplane.SyncLocked(claudiaDB, jevonsDB); err != nil {
		return 0, err
	}
	if err := controlplane.SyncLocked(claudiaDB, jevonsDB); err != nil {
		return 0, err
	}
	return 1, nil
}

func dropReplicaHandshake(claudiaDB, jevonsDB *sqlpipe.Database) (int, error) {
	client, err := sqlpipe.NewPeer(claudiaDB, sqlpipe.PeerConfig{OwnedTables: controlplane.ClaudiaTables()})
	if err != nil {
		return 0, err
	}
	defer client.Close()
	server, err := sqlpipe.NewPeer(jevonsDB, sqlpipe.PeerConfig{ApproveOwnership: controlplane.ApproveClaudia})
	if err != nil {
		return 0, err
	}
	defer server.Close()
	pending, err := client.Start()
	if err != nil {
		return 0, err
	}
	return len(pending), nil
}
