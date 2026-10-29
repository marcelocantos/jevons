// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package fleetpanel classifies registry rows for the owner-facing fleet tree
// (🎯T1121): live seats, genuine parks, faults, and history (reaped/killed).
package fleetpanel

import (
	"strings"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
)

// Class is how a seat earns a row in the default fleet panel.
type Class string

const (
	ClassLive    Class = "live"
	ClassPark    Class = "park"
	ClassFault   Class = "fault"
	ClassHistory Class = "history"
)

// StatusRunningAgainstIntent is the list/API status for a live seat whose
// recorded intent stood it down (🎯T1127 / 🎯T788), distinct from plain running.
const StatusRunningAgainstIntent = "intent_fault"

// Row is the panel projection for one registered seat.
type Row struct {
	Class   Class
	Visible bool
	// Line is one owner-facing sentence for park/fault rows (🎯T1059 / 🎯T751).
	Line string
}

// Input is everything the classifier needs; it does not read the registry.
type Input struct {
	Running  bool
	Starting bool
	// Unobserved: nothing has said whether the seat is alive yet (seatstate
	// Alive=Unknown), as for every seat in the minute after a restart.
	Unobserved bool
	AutoStart  bool
	// AutoHold is the daemon's current policy refusal to revive an auto seat.
	AutoHold string
	Intent   fleetintent.State
	Record   fleetintent.Record
	// Seat stop reason from the seat-stop ledger (fleetlog vocabulary or prose).
	StopReason string
	StoppedAt  string
}

// Classify decides whether a seat belongs in the default fleet panel.
func Classify(in Input) Row {
	intent := fleetintent.Resolve(in.Intent)
	stop := strings.TrimSpace(in.StopReason)
	intentStop := strings.TrimSpace(in.Record.StopReason)

	if in.Running || in.Starting {
		if fleetintent.StoodDown(intent) {
			return Row{
				Class:   ClassFault,
				Visible: true,
				Line:    faultRunningAgainstIntent(in),
			}
		}
		return Row{Class: ClassLive, Visible: true}
	}

	if isHistory(intent, intentStop, stop) {
		return Row{Class: ClassHistory, Visible: false}
	}

	switch intent {
	case fleetintent.Parked, fleetintent.BlockedOwner, fleetintent.BlockedProvider:
		return Row{Class: ClassPark, Visible: true, Line: parkLine(in)}
	}

	// Not knowing is not evidence of a stop: a seat whose intent is runnable
	// and whose liveness nobody has observed stays on the panel. Hiding it
	// hid live workers after every restart.
	if in.Unobserved && in.AutoHold == "" && fleetintent.Runnable(intent) {
		return Row{Class: ClassLive, Visible: true}
	}

	if in.AutoStart && in.AutoHold == "" && fleetintent.Runnable(intent) {
		return Row{Class: ClassFault, Visible: true, Line: faultDeadDurable(in)}
	}

	// Stopped work rows with no external block are resume handles, not decisions.
	return Row{Class: ClassHistory, Visible: false}
}

func isHistory(intent fleetintent.State, intentStop, seatStop string) bool {
	if intent == fleetintent.Reaped {
		return true
	}
	if fleetintent.IsFinishedWorkStopReason(intentStop) {
		return true
	}
	switch strings.TrimSpace(seatStop) {
	case fleetlog.ReasonKill, fleetlog.ReasonReapDone, fleetlog.ReasonReapAchieve,
		fleetlog.ReasonReapStop, fleetlog.ReasonDeadSeat, fleetlog.ReasonUnbriefedSeat,
		fleetlog.ReasonBudgetKill, fleetlog.ReasonAsideDismiss, fleetlog.ReasonThreadRemove:
		return true
	}
	low := strings.ToLower(seatStop)
	if strings.Contains(low, "finished and reaped") ||
		strings.Contains(low, "reaped on achieve") ||
		strings.Contains(low, "killed by explicit request") {
		return true
	}
	return false
}

func parkLine(in Input) string {
	if r := strings.TrimSpace(in.Record.Reason); r != "" {
		return r
	}
	return fleetintent.Describe(in.Intent)
}

func faultDeadDurable(in Input) string {
	if t := strings.TrimSpace(in.StoppedAt); t != "" {
		return "should be running; stopped since " + t
	}
	return "should be running; seat is down"
}

func faultRunningAgainstIntent(in Input) string {
	return "should not be running (" + fleetintent.Describe(in.Intent) + ")"
}

// ServedListStatus maps an observed lifecycle phase to the status served on
// jevons_agent_list and GET /api/agents when a live process runs against a
// stood-down intent (🎯T1127). Dead durable faults keep the observed status.
func ServedListStatus(processUp, starting bool, intent fleetintent.State, observed string) string {
	if (processUp || starting) && fleetintent.StoodDown(intent) {
		return StatusRunningAgainstIntent
	}
	return observed
}
