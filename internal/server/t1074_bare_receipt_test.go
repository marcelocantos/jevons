// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// prepareBareSeat registers a stub worker and does not launch it. A send
// through the bare registry path rehydrates. launch=true is the already-live
// seat, which must not be reported as rehydrated.
func prepareBareSeat(t *testing.T, s *Server, name string, launch bool) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: filepath.Join(dir, "work"), SessionID: t1074FallbackSession,
		TargetID: "T-bare-receipt", Provider: claudia.ProviderGrok, Materialized: true, AutoStart: false,
	}); err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(context.Context, claudia.Config) (*claudia.Agent, error) {
			return claudia.NewStubAgent(func(string) error { return nil }), nil
		},
	})
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	if !launch {
		t.Cleanup(func() { reg.Stop(name) })
		return
	}
	if _, err := reg.Launch(name); err != nil {
		t.Fatalf("stub launch: %v", err)
	}
	t.Cleanup(func() { reg.Stop(name) })
	s.seats.Load().FromClaudia(seatstate.SeatReport{
		Name: name, SessionID: t1074FallbackSession, Alive: true, Known: true,
	}, time.Now())
}

func installBareReceipt(t *testing.T, s *Server, name string, state claudia.ReceiptState, reason string) *int {
	t.Helper()
	calls := 0
	s.SetSendReceiptQuery(func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		calls++
		if q.Seat != name || q.SessionID != t1074FallbackSession || !strings.HasPrefix(q.RequestID, "rq-") {
			t.Errorf("bare receipt query = %+v", q)
		}
		return durableOwnerReceipt(q, state, reason), nil
	})
	return &calls
}

func TestT1074BareRegistryNilQueryDoesNotConfirm(t *testing.T) {
	const name = "jv-t1074-bare-nil"
	s := &Server{}
	prepareBareSeat(t, s, name, true)
	start := time.Now()
	out, err := s.sendToNamedAgentMode(name, "alpha", sendOriginOwner, delivery.ModeSubmit)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("nil query took %s", time.Since(start))
	}
	if out.Status == "sent" || out.Status == "rehydrated_sent" || out.Status != "delivered_unconfirmed" {
		t.Fatalf("nil query status = %+v", out)
	}
}

func TestT1074BareRegistryUnknownReceiptDoesNotConfirm(t *testing.T) {
	const name = "jv-t1074-bare-unknown"
	s := &Server{}
	prepareBareSeat(t, s, name, true)
	calls := installBareReceipt(t, s, name, claudia.ReceiptUnknown, "401 unauthorized")
	out, err := s.sendToNamedAgentMode(name, "alpha", sendOriginOwner, delivery.ModeSubmit)
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 1 || out.Status != "delivered_unconfirmed" {
		t.Fatalf("calls=%d status=%+v", *calls, out)
	}
}

func TestT1074BareRegistryQueryErrorDoesNotConfirm(t *testing.T) {
	const name = "jv-t1074-bare-err"
	s := &Server{}
	prepareBareSeat(t, s, name, true)
	calls := 0
	s.SetSendReceiptQuery(func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		calls++
		if q.Seat != name || q.SessionID != t1074FallbackSession || !strings.HasPrefix(q.RequestID, "rq-") {
			t.Errorf("query = %+v", q)
		}
		return nil, fmt.Errorf("receipt store closed")
	})
	out, err := s.sendToNamedAgentMode(name, "alpha", sendOriginOwner, delivery.ModeSubmit)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || out.Status != "delivered_unconfirmed" {
		t.Fatalf("calls=%d status=%+v", calls, out)
	}
}

func TestT1074BareRegistryForeignReceiptDoesNotConfirm(t *testing.T) {
	const name = "jv-t1074-bare-foreign"
	s := &Server{}
	prepareBareSeat(t, s, name, true)
	s.SetSendReceiptQuery(func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		r := durableOwnerReceipt(q, claudia.ReceiptCompleted, "401 unauthorized")
		r.RequestID = "other-request"
		return r, nil
	})
	out, err := s.sendToNamedAgentMode(name, "alpha", sendOriginOwner, delivery.ModeSubmit)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "delivered_unconfirmed" {
		t.Fatalf("foreign receipt status = %+v", out)
	}
}

func TestT1074BareRegistrySettlingReceiptConfirms(t *testing.T) {
	const name = "jv-t1074-bare-settled"
	s := &Server{}
	prepareBareSeat(t, s, name, true)
	calls := installBareReceipt(t, s, name, claudia.ReceiptCompleted, "401 unauthorized")
	out, err := s.sendToNamedAgentMode(name, "alpha", sendOriginOwner, delivery.ModeSubmit)
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 1 || out.Status != "sent" {
		t.Fatalf("calls=%d status=%+v", *calls, out)
	}
}

func TestT1074BareRegistryRehydratedNilQueryDoesNotConfirm(t *testing.T) {
	const name = "jv-t1074-bare-rehydrate-nil"
	s := &Server{}
	prepareBareSeat(t, s, name, false)
	start := time.Now()
	out, err := s.sendToNamedAgentMode(name, "alpha", sendOriginOwner, delivery.ModeSubmit)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("nil query took %s", time.Since(start))
	}
	if out.Status == "sent" || out.Status == "rehydrated_sent" || out.Status != "delivered_unconfirmed" {
		t.Fatalf("rehydrated nil query status = %+v", out)
	}
}

func TestT1074BareRegistryRehydratedSettlingReceiptConfirms(t *testing.T) {
	const name = "jv-t1074-bare-rehydrate-settled"
	s := &Server{}
	prepareBareSeat(t, s, name, false)
	calls := installBareReceipt(t, s, name, claudia.ReceiptCompleted, "exact recorded outcome")
	out, err := s.sendToNamedAgentMode(name, "alpha", sendOriginOwner, delivery.ModeSubmit)
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 1 || out.Status != "rehydrated_sent" {
		t.Fatalf("calls=%d status=%+v", *calls, out)
	}
}
