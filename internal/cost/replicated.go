// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/marcelocantos/jevons/internal/claudiawire"
)

// ApplyReplicatedUsage ingests one Claudia-owned usage fact delivered by a
// sqlpipe subscription or read back from the Jevons replica. Repeated delivery
// of the same fact does not add usage.
func ApplyReplicatedUsage(ctx context.Context, s *Store, factJSON string) error {
	if s == nil {
		return fmt.Errorf("replicated usage: nil store")
	}
	var fact claudiawire.UsageFact
	if err := json.Unmarshal([]byte(factJSON), &fact); err != nil {
		return fmt.Errorf("%w: replicated usage: %w", ErrUsageFactRejected, err)
	}
	return s.IngestUsageFact(ctx, fact.Seat, fact.SessionID, &fact)
}

// ApplyReplicatedReceipt checks a Claudia-owned receipt row. The logical
// request id is the operation identity; the JSON is not a second journal.
func ApplyReplicatedReceipt(factJSON string) (claudiawire.RequestReceipt, error) {
	var receipt claudiawire.RequestReceipt
	if err := json.Unmarshal([]byte(factJSON), &receipt); err != nil {
		return claudiawire.RequestReceipt{}, fmt.Errorf("replicated receipt: %w", err)
	}
	if err := receipt.ValidateIdentity(receipt.Seat, receipt.SessionID, receipt.RequestID); err != nil {
		return claudiawire.RequestReceipt{}, err
	}
	return receipt, nil
}
