// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// The kill-switch reaches seats the registry lost (unowned on the broker),
// leaves owned seats to StopFleet, and never touches a protected name.
func TestBrokerKillSwitchStopsUnownedUnprotectedSeats(t *testing.T) {
	var stopped []string
	k := &BrokerKillSwitch{
		List: func(context.Context) ([]BrokerSeat, error) {
			return []BrokerSeat{
				{Name: "lost-worker"}, {Name: "owned-worker", Owned: true},
				{Name: "jevons"}, {Name: "other-lost"},
			}, nil
		},
		Stop:    func(_ context.Context, n string) error { stopped = append(stopped, n); return nil },
		Protect: []string{"jevons"},
	}
	if err := k.Kill(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(stopped)
	if !slices.Equal(stopped, []string{"lost-worker", "other-lost"}) {
		t.Fatalf("stopped %v", stopped)
	}
}

func TestBrokerKillSwitchReportsListAndStopFailures(t *testing.T) {
	k := &BrokerKillSwitch{
		List: func(context.Context) ([]BrokerSeat, error) { return nil, errors.New("no broker") },
		Stop: func(context.Context, string) error { return nil },
	}
	if err := k.Kill(); err == nil {
		t.Fatal("list failure swallowed")
	}
	k.List = func(context.Context) ([]BrokerSeat, error) { return []BrokerSeat{{Name: "a"}}, nil }
	k.Stop = func(context.Context, string) error { return errors.New("refused") }
	if err := k.Kill(); err == nil {
		t.Fatal("stop failure swallowed")
	}
	if err := (&BrokerKillSwitch{}).Kill(); err == nil {
		t.Fatal("unwired switch reported success")
	}
}
