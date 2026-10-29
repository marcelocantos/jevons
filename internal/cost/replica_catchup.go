// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"database/sql"
	"fmt"
)

// replicaCatchupSchema records which replicated usage rows have been
// ingested into the accounting projection. It is bookkeeping on the usage
// store, not Claudia's journal.
const replicaCatchupSchema = `
CREATE TABLE IF NOT EXISTS replica_catchup_meta (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	bootstrapped INTEGER NOT NULL CHECK (bootstrapped IN (0, 1))
);
INSERT OR IGNORE INTO replica_catchup_meta (id, bootstrapped) VALUES (1, 0);
CREATE TABLE IF NOT EXISTS replica_catchup_applied (
	seat TEXT NOT NULL,
	session_id TEXT NOT NULL,
	seq INTEGER NOT NULL CHECK (seq > 0),
	fact_json TEXT NOT NULL,
	PRIMARY KEY (seat, session_id, seq)
);
`

// DBPath returns the filesystem path of this store's database file.
// Empty for :memory: databases.
func (s *Store) DBPath() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *Store) replicaCatchupBootstrapped(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT bootstrapped FROM replica_catchup_meta WHERE id = 1`).Scan(&n)
	if err == sql.ErrNoRows {
		return false, fmt.Errorf("replica catch-up: missing meta row")
	}
	if err != nil {
		return false, err
	}
	if n != 0 && n != 1 {
		return false, fmt.Errorf("replica catch-up: malformed bootstrapped flag %d", n)
	}
	return n == 1, nil
}

func (s *Store) setReplicaCatchupBootstrapped(ctx context.Context, bootstrapped bool) error {
	v := 0
	if bootstrapped {
		v = 1
	}
	res, err := s.db.ExecContext(ctx, `UPDATE replica_catchup_meta SET bootstrapped = ? WHERE id = 1`, v)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("replica catch-up: missing meta row")
	}
	return nil
}

func (s *Store) markReplicaCatchupApplied(ctx context.Context, seat, session string, seq int64, factJSON string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO replica_catchup_applied (seat, session_id, seq, fact_json) VALUES (?, ?, ?, ?)
		ON CONFLICT(seat, session_id, seq) DO UPDATE SET fact_json = excluded.fact_json`,
		seat, session, seq, factJSON)
	return err
}

func (s *Store) markReplicaCatchupAppliedBatch(ctx context.Context, rows []replicaUsageRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO replica_catchup_applied (seat, session_id, seq, fact_json) VALUES (?, ?, ?, ?)
		ON CONFLICT(seat, session_id, seq) DO UPDATE SET fact_json = excluded.fact_json`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if _, err := stmt.ExecContext(ctx, row.seat, row.session, row.seq, row.body); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// replicaUsageRow is one replicated usage fact row from Jevons' file.
type replicaUsageRow struct {
	seat, session, body string
	seq                 int64
}
