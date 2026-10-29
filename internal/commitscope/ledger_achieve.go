// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/marcelocantos/jevons/internal/roles"
)

// NewlyAchievedTargets returns target IDs whose staged record shows status
// achieved when HEAD did not (🎯T1160).
func NewlyAchievedTargets(head, staged []byte) ([]string, error) {
	h, err := parseTargets(head)
	if err != nil {
		return nil, err
	}
	s, err := parseTargets(staged)
	if err != nil {
		return nil, err
	}
	var out []string
	for id, sBlob := range s {
		if targetStatus(sBlob) != "achieved" {
			continue
		}
		if targetStatus(h[id]) == "achieved" {
			continue
		}
		out = append(out, id)
	}
	return out, nil
}

func targetStatus(blob []byte) string {
	if len(blob) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(blob, &m); err != nil {
		return ""
	}
	v, _ := m["status"].(string)
	return strings.ToLower(strings.TrimSpace(v))
}

// MayWriteLedgerAchieve reports whether actor may land a ledger achieve commit
// (🎯T1160). Empty actor means unattributed — integrate, ledgercommit, or a
// human — and is allowed. Role and purpose come from JEVONS_ROLE /
// JEVONS_PURPOSE when set; otherwise durable PO seat names are recognized.
func MayWriteLedgerAchieve(actor, role, purpose string) bool {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return true
	}
	switch roles.OfSeat(role, purpose) {
	case roles.ProductOwner, roles.Overseer, roles.Boss:
		return true
	case roles.Worker:
		return false
	default:
		if strings.HasPrefix(actor, "jv-") {
			return false
		}
		if strings.HasSuffix(actor, "-po") {
			return true
		}
		return actor == "jevons"
	}
}

// WorkerLedgerAchieveRefusal decides whether a fleet work seat must not commit
// a bullseye achieve (🎯T1160).
func WorkerLedgerAchieveRefusal(actor, role, purpose string, head, staged []byte) (refuse bool, message string) {
	if MayWriteLedgerAchieve(actor, role, purpose) {
		return false, ""
	}
	ids, err := NewlyAchievedTargets(head, staged)
	if err != nil || len(ids) == 0 {
		return false, ""
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "fleet seat"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "commitscope: seat %q refused ledger achieve for %s (🎯T1160).\n\n", actor, strings.Join(ids, ", "))
	b.WriteString("Achieve is reserved for product owners after independent SHA+gate verification.\n")
	b.WriteString("Workers land code through `go run ./cmd/integrate` and report finish evidence\n")
	b.WriteString("to their parent PO — they do not write achieve rows themselves.\n\n")
	b.WriteString("When integrate refuses a landing, flag the refusal to your parent and park\n")
	b.WriteString("(jevons: status blocked); never hand-commit onto the shared integration branch.\n")
	return true, b.String()
}
