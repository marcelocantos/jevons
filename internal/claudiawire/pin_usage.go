// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !jevons_claudia_next

package claudiawire

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"
)

// Copied from Claudia omp at 2b10c121. The published v0.53.0 module does not
// export these types. The jevons_claudia_next build aliases Claudia instead.
// This copy is not a control-plane client and is not 🎯T1074.9 evidence.

const UsagePageLimit = 128

// UsageCounters preserves availability per field. Nil means unknown; a
// pointer to zero means an explicitly observed zero.
type UsageCounters struct {
	TotalInputTokens         *int64 `json:"total_input_tokens,omitempty"`
	InputTokens              *int64 `json:"input_tokens,omitempty"`
	OutputTokens             *int64 `json:"output_tokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens,omitempty"`
	ReasoningTokens          *int64 `json:"reasoning_tokens,omitempty"`
}

type UsageRequestRef struct {
	RequestID string `json:"request_id"`
	AttemptID string `json:"attempt_id"`
}

// UsageFact declares whether it observes an engine invocation or a physical
// provider attempt.
type UsageFact struct {
	InvocationID            string            `json:"invocation_id,omitempty"`
	ProviderAttemptID       string            `json:"provider_attempt_id,omitempty"`
	ProviderRequestID       string            `json:"provider_request_id,omitempty"`
	ProviderResponseID      string            `json:"provider_response_id,omitempty"`
	Seq                     uint64            `json:"seq"`
	CallID                  string            `json:"call_id"`
	Seat                    string            `json:"seat"`
	SessionID               string            `json:"session_id"`
	Kind                    string            `json:"kind"`
	State                   string            `json:"state"`
	Provider                string            `json:"provider"`
	Model                   string            `json:"model"`
	TurnID                  string            `json:"turn_id,omitempty"`
	Requests                []UsageRequestRef `json:"requests"`
	Cause                   string            `json:"cause,omitempty"`
	StartedAt               string            `json:"started_at"`
	FinishedAt              string            `json:"finished_at,omitempty"`
	Counters                UsageCounters     `json:"counters"`
	ContextTokens           *int64            `json:"context_tokens,omitempty"`
	APIEquivalentCostUSD    *float64          `json:"api_equivalent_cost_usd,omitempty"`
	CounterSource           string            `json:"counter_source"`
	CostSource              string            `json:"cost_source,omitempty"`
	Coverage                string            `json:"coverage"`
	ProtocolTokenDeltaTotal *int64            `json:"protocol_token_delta_total,omitempty"`
}

func (f *UsageFact) ValidateIdentity(seat, session string) error {
	if f == nil || f.Seat != seat || f.SessionID != session || strings.TrimSpace(seat) == "" || strings.TrimSpace(session) == "" || f.Seq == 0 || f.CallID == "" || f.Provider == "" || f.Model == "" || f.Requests == nil {
		return fmt.Errorf("usage fact: invalid identity or coverage")
	}
	invocation := f.Coverage == "engine_invocation" && f.CounterSource == "adapter_normalized_positive" && f.InvocationID == "" && f.ProviderAttemptID == ""
	attempt := f.Coverage == "provider_attempt" && f.CounterSource == "provider_reported" && strings.TrimSpace(f.InvocationID) != "" && f.ProviderAttemptID == f.CallID
	if !invocation && !attempt {
		return fmt.Errorf("usage fact: invalid observation scope")
	}
	if f.Kind != "model" && f.Kind != "compaction" {
		return fmt.Errorf("usage fact: invalid invocation kind")
	}
	start, err := time.Parse(time.RFC3339Nano, f.StartedAt)
	if err != nil {
		return fmt.Errorf("usage fact: invalid start time")
	}
	switch f.State {
	case "started":
		if f.FinishedAt != "" || f.Counters != (UsageCounters{}) || f.ContextTokens != nil || f.ProtocolTokenDeltaTotal != nil || f.APIEquivalentCostUSD != nil {
			return fmt.Errorf("usage fact: unfinished invocation has counters")
		}
	case "completed", "failed", "cancelled", "unknown":
		finish, err := time.Parse(time.RFC3339Nano, f.FinishedAt)
		if err != nil || finish.Before(start) {
			return fmt.Errorf("usage fact: invalid finish time")
		}
	default:
		return fmt.Errorf("usage fact: invalid state")
	}
	seen := map[string]bool{}
	for _, r := range f.Requests {
		if r.RequestID == "" || r.AttemptID == "" || seen[r.RequestID] {
			return fmt.Errorf("usage fact: invalid request reference")
		}
		seen[r.RequestID] = true
	}
	for _, n := range []*int64{f.Counters.TotalInputTokens, f.Counters.InputTokens, f.Counters.OutputTokens, f.Counters.CacheReadInputTokens, f.Counters.CacheCreationInputTokens, f.Counters.ReasoningTokens, f.ContextTokens, f.ProtocolTokenDeltaTotal} {
		if n != nil && (*n < 0 || *n > 1<<53-1) {
			return fmt.Errorf("usage fact: invalid counter")
		}
	}
	if c := f.APIEquivalentCostUSD; c != nil && (math.IsNaN(*c) || math.IsInf(*c, 0) || *c < 0 || f.CostSource != "provider_adapter_estimate") {
		return fmt.Errorf("usage fact: invalid cost estimate")
	}
	return nil
}

type UsagePage struct {
	Seat      string      `json:"seat"`
	SessionID string      `json:"session_id"`
	Facts     []UsageFact `json:"facts"`
	Next      uint64      `json:"next"`
	More      bool        `json:"more"`
}

func (p *UsagePage) Validate(seat, session string, after uint64, limit int) error {
	if p == nil || strings.TrimSpace(seat) == "" || strings.TrimSpace(session) == "" || limit < 1 || limit > UsagePageLimit || after > 1<<53-1 || p.Seat != seat || p.SessionID != session || p.Facts == nil || len(p.Facts) > limit || p.Next != after+uint64(len(p.Facts)) || (p.More && len(p.Facts) == 0) {
		return fmt.Errorf("usage page: invalid identity or cursor")
	}
	for i := range p.Facts {
		if p.Facts[i].Seq != after+uint64(i)+1 {
			return fmt.Errorf("usage page: noncontiguous sequence")
		}
		if err := p.Facts[i].ValidateIdentity(seat, session); err != nil {
			return err
		}
	}
	return nil
}

type UsageQuery struct {
	Seat, SessionID string
	After           uint64
	Limit           int
}

// ValidateUsageTransition decides whether incoming replaces a projection's
// previous fact for one call. Copied from Claudia omp at 2b10c121.
func ValidateUsageTransition(previous, incoming *UsageFact) (bool, error) {
	if incoming == nil {
		return false, fmt.Errorf("usage transition: missing incoming fact")
	}
	if err := incoming.ValidateIdentity(incoming.Seat, incoming.SessionID); err != nil {
		return false, fmt.Errorf("usage transition incoming: %w", err)
	}
	if previous == nil {
		return true, nil
	}
	if err := previous.ValidateIdentity(previous.Seat, previous.SessionID); err != nil {
		return false, fmt.Errorf("usage transition previous: %w", err)
	}
	identity := func(f UsageFact) UsageFact {
		f.Seq, f.State, f.FinishedAt = 0, "", ""
		f.Counters = UsageCounters{}
		f.ContextTokens, f.ProtocolTokenDeltaTotal, f.APIEquivalentCostUSD = nil, nil, nil
		f.CostSource, f.ProviderRequestID, f.ProviderResponseID = "", "", ""
		return f
	}
	if !reflect.DeepEqual(identity(*previous), identity(*incoming)) {
		return false, fmt.Errorf("usage transition: immutable identity changed")
	}
	if incoming.Seq == previous.Seq {
		if !reflect.DeepEqual(*previous, *incoming) {
			return false, fmt.Errorf("usage transition: same sequence has conflicting facts")
		}
		return false, nil
	}
	if incoming.Seq < previous.Seq {
		if incoming.State == "started" && previous.State != "started" {
			return false, nil
		}
		return false, fmt.Errorf("usage transition: invalid older replay")
	}
	if previous.State != "started" || incoming.State == "started" {
		return false, fmt.Errorf("usage transition: expected started to terminal")
	}
	return true, nil
}
