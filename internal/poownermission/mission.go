// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package poownermission tracks bounded owner requests to a product owner
// (🎯T1195): typed request id plus item list or count, wake while open,
// close on a typed PO finish or blocked report.
package poownermission

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// MinListedItems is the smallest explicit target-id list that counts as
// bounded without an owner-mission fence (structural tokens, not prose).
const MinListedItems = 2

// Mission is one open owner obligation for a product owner.
type Mission struct {
	RequestID string    `json:"request_id"`
	PO        string    `json:"po"`
	Items     []string  `json:"items,omitempty"`
	ItemCount int       `json:"item_count,omitempty"`
	OpenedAt  time.Time `json:"opened_at"`
	LastWake  time.Time `json:"last_wake,omitzero"`
}

// BoundedRequest is a parsed owner→PO bounded mission intake.
type BoundedRequest struct {
	RequestID string
	Items     []string
	ItemCount int
}

// ParseBoundedRequest records a mission only when the transport names a
// request id and the body carries a typed item list or count — never from
// prose alone.
func ParseBoundedRequest(text, requestID string) (BoundedRequest, bool) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return BoundedRequest{}, false
	}
	if br, ok := parseOwnerMissionFence(text, requestID); ok {
		return br, true
	}
	ids := fleet.GoalTargetIDs(text)
	if len(ids) >= MinListedItems {
		return BoundedRequest{RequestID: requestID, Items: ids}, true
	}
	return BoundedRequest{}, false
}

func parseOwnerMissionFence(text, requestID string) (BoundedRequest, bool) {
	m, _ := envelope.Parse(text)
	if m == nil || m.Kind != envelope.KindOwnerMission {
		return BoundedRequest{}, false
	}
	br := BoundedRequest{RequestID: requestID}
	if raw := strings.TrimSpace(m.Extra["items"]); raw != "" {
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == ';'
		}) {
			id := normalizeTargetID(part)
			if id != "" {
				br.Items = append(br.Items, id)
			}
		}
	}
	if raw := strings.TrimSpace(firstNonEmpty(m.Extra["item-count"], m.Extra["count"])); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return BoundedRequest{}, false
		}
		br.ItemCount = n
	}
	if len(br.Items) == 0 && br.ItemCount == 0 {
		return BoundedRequest{}, false
	}
	return br, true
}

func normalizeTargetID(raw string) string {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "🎯"))
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToUpper(raw), "T") {
		return ""
	}
	return strings.ToUpper(raw)
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// RemainingItems returns mission items still open on the ledger.
func RemainingItems(workdir string, m Mission) []string {
	if len(m.Items) == 0 {
		return nil
	}
	var out []string
	for _, id := range m.Items {
		id = normalizeTargetID(id)
		if id == "" {
			continue
		}
		if workdir == "" {
			out = append(out, id)
			continue
		}
		st, ok := targetfile.LoadTargetStatusFromCwd(workdir, id)
		if !ok || targetStillOpen(st) {
			out = append(out, id)
		}
	}
	return out
}

func targetStillOpen(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "achieved", "set_aside", "retired", "done":
		return false
	default:
		return true
	}
}

// RemainingCount is the best-effort remaining obligation size.
func RemainingCount(workdir string, m Mission) int {
	if len(m.Items) > 0 {
		return len(RemainingItems(workdir, m))
	}
	if m.ItemCount > 0 {
		return m.ItemCount
	}
	return 0
}

// POObs is the product-owner side of the wake read.
type POObs struct {
	Name          string
	Alive         bool
	Phase         string
	GraceElapsed  bool
	EngagedChild  int
	LastTurnEnded time.Time
}

// Verdict classifies one PO against an open owner mission.
type Verdict string

const (
	VerdictOK          Verdict = "ok"
	VerdictWithinGrace Verdict = "within_grace"
	VerdictPOWorking   Verdict = "po_working"
	VerdictNoMission   Verdict = "no_mission"
	VerdictChildren    Verdict = "children_working"
	VerdictWake        Verdict = "wake"
)

// IsFault reports whether the PO should be woken for the open mission.
func IsFault(v Verdict) bool { return v == VerdictWake }

// Classify names whether the PO should be woken from daemon-held observations.
func Classify(po POObs, m *Mission, workdir string) (Verdict, string) {
	if m == nil || strings.TrimSpace(m.RequestID) == "" {
		return VerdictNoMission, ""
	}
	if !po.Alive {
		return VerdictOK, ""
	}
	if !phaseIdle(po.Phase) {
		return VerdictPOWorking, ""
	}
	if po.EngagedChild > 0 {
		return VerdictChildren, ""
	}
	if !po.GraceElapsed {
		return VerdictWithinGrace, ""
	}
	return VerdictWake, formatDetail(po.Name, *m, workdir)
}

func phaseIdle(phase string) bool {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "", "idle":
		return true
	default:
		return false
	}
}

func formatDetail(po string, m Mission, workdir string) string {
	rem := RemainingCount(workdir, m)
	if len(m.Items) > 0 {
		return fmt.Sprintf("po=%s owner_mission=%s remaining_items=%d/%d",
			po, m.RequestID, rem, len(m.Items))
	}
	return fmt.Sprintf("po=%s owner_mission=%s remaining_count=%d item_count=%d",
		po, m.RequestID, rem, m.ItemCount)
}

// FormatWakeBody is the owner-facing enumeration for the PO wake message.
func FormatWakeBody(m Mission, workdir string) string {
	if strings.TrimSpace(m.RequestID) == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "request_id=%s", m.RequestID)
	if len(m.Items) > 0 {
		rem := RemainingItems(workdir, m)
		fmt.Fprintf(&b, " remaining=%d total=%d", len(rem), len(m.Items))
		if len(rem) > 0 && len(rem) <= 12 {
			b.WriteString(" items=")
			b.WriteString(strings.Join(rem, ","))
		} else if len(rem) > 12 {
			b.WriteString(" sample=")
			b.WriteString(strings.Join(rem[:8], ","))
			b.WriteString(",…")
		}
	} else if m.ItemCount > 0 {
		fmt.Fprintf(&b, " item_count=%d", m.ItemCount)
	}
	return b.String()
}

// ClosesMission reports whether a PO terminal report closes the open mission.
func ClosesMission(text string) bool {
	m, err := envelope.Parse(text)
	if m == nil || m.Kind != envelope.KindFinishReport || err != nil {
		return false
	}
	if m.HasActiveWait() {
		return false
	}
	if m.Status == envelope.ProgressBlocked || m.IsBlocked() {
		return strings.TrimSpace(m.Blocker) != ""
	}
	if m.Status == envelope.ProgressInProgress {
		return false
	}
	return m.HasOracle() || m.HasRisk()
}
