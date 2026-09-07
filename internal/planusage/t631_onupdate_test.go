// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/planusage"
)

func TestRefreshNotifiesOnSuccessAndFailure(t *testing.T) {
	var n int
	ok := planusage.NewReader(planusage.ReaderArgs{
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			return []claudia.PlanUsage{{
				Provider:  claudia.ProviderClaude,
				Status:    claudia.PlanUsageAvailable,
				FetchedAt: time.Now(),
				Windows: []claudia.PlanWindow{{
					Name:             claudia.PlanWindowWeekly,
					RemainingPercent: pct(40),
				}},
			}}, nil
		},
		OnUpdate: func() { n++ },
	})
	if err := ok.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("success OnUpdate=%d want 1", n)
	}

	fail := planusage.NewReader(planusage.ReaderArgs{
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			return nil, errors.New("provider down")
		},
		OnUpdate: func() { n++ },
	})
	if err := fail.Refresh(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if n != 2 {
		t.Fatalf("failure OnUpdate=%d want 2", n)
	}
}
