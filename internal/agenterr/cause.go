// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import (
	"errors"

	"github.com/marcelocantos/claudia"
)

// Provider-failure causes. These spellings match claudia.ProviderCause.
// A control switches on the code. It does not read the rendered sentence.
const (
	CauseTurnInFlight       = "turn_in_flight"
	CauseProcessNotRunning  = "process_not_running"
	CauseBusy               = "busy"
	CauseSteerUnsupported   = "steer_unsupported"
	CauseTurnIdle           = "turn_idle"
	CauseAuth               = "auth"
	CauseRateLimit          = "rate_limit"
	CauseSpendWall          = "spend_wall"
	CauseAccountWall        = "account_wall"
	CauseBackendUnavailable = "backend_unavailable"
	CauseClientBug          = "client_bug"
	CauseRetryExhausted     = "retry_exhausted"
	CauseContextOverflow    = "context_overflow"
)

// causedFailure carries a provider-failure cause without changing the
// rendered text. A control reads the cause. The sentence is not one.
type causedFailure struct {
	cause string
	text  string
	next  error
}

func (e *causedFailure) Error() string {
	if e == nil {
		return ""
	}
	return e.text
}

func (e *causedFailure) ProviderCause() string {
	if e == nil {
		return ""
	}
	return e.cause
}

func (e *causedFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.next
}

// WithCause attaches cause to err. The rendered text stays err.Error().
// An empty cause or a nil error is returned unchanged.
func WithCause(cause string, err error) error {
	if err == nil || cause == "" {
		return err
	}
	return &causedFailure{cause: cause, text: err.Error(), next: err}
}

// ProviderCause reads a typed provider-failure cause.
// An error that only renders one of these sentences has no cause.
// A direct claudia sentinel still answers, because that value is the cause
// even when an older broker flattened it to text on the other path.
func ProviderCause(err error) string {
	if err == nil {
		return ""
	}
	var c interface{ ProviderCause() string }
	if errors.As(err, &c) && c != nil {
		if cause := c.ProviderCause(); cause != "" {
			return cause
		}
	}
	switch {
	case errors.Is(err, claudia.ErrTurnInFlight):
		return CauseTurnInFlight
	case errors.Is(err, claudia.ErrSteerUnsupported):
		return CauseSteerUnsupported
	case errors.Is(err, claudia.ErrTurnIdle):
		return CauseTurnIdle
	default:
		return ""
	}
}
