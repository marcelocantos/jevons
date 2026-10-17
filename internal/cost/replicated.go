// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/marcelocantos/claudia"
)

// ApplyReplicatedUsage ingests one Claudia-owned usage fact delivered by a
// sqlpipe subscription or read back from the Jevons replica. Repeated delivery
// of the same fact does not add usage.
func ApplyReplicatedUsage(ctx context.Context, s *Store, factJSON string) error {
	if s == nil {
		return fmt.Errorf("replicated usage: nil store")
	}
	var fact claudia.UsageFact
	if err := json.Unmarshal([]byte(factJSON), &fact); err != nil {
		return fmt.Errorf("replicated usage: %w", err)
	}
	return s.IngestUsageFact(ctx, fact.Seat, fact.SessionID, &fact)
}

// ApplyReplicatedReceipt checks a Claudia-owned receipt row. The logical
// request id is the operation identity; the JSON is not a second journal.
func ApplyReplicatedReceipt(factJSON string) (claudia.RequestReceipt, error) {
	var receipt claudia.RequestReceipt
	if err := json.Unmarshal([]byte(factJSON), &receipt); err != nil {
		return claudia.RequestReceipt{}, fmt.Errorf("replicated receipt: %w", err)
	}
	if err := receipt.ValidateIdentity(receipt.Seat, receipt.SessionID, receipt.RequestID); err != nil {
		return claudia.RequestReceipt{}, err
	}
	return receipt, nil
}
