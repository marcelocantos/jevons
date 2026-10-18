// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import (
	"fmt"
	"strings"
)

// Class is a structured provider/ACP failure class (🎯T237).
// Stable short codes for slog/eventlog and for T236 recovery — never
// scrape owner-visible prose for control decisions.
//
// Enumerated classes:
//   - backend_unavailable: cloud/provider outage, 5xx, Internal error, timeout
//   - rate_limit: 429 / quota / throttle. Ordinary throttle stays transient.
//   - spend_wall: account spend or billing refusal. Not a 429.
//   - account_wall: revoked key or blocked account that is not an auth class.
//   - auth: 401 / 403 / API key / unauthorized
//   - client_bug: local config, wire, session, bad request
//   - retry_exhausted: the CLI's own retry budget ran out on a transient
//     step-level failure (🎯T862.12) — not the same as the transient
//     backend_unavailable/rate_limit cause that first triggered the retry
//   - context_overflow: the prompt is longer than the model's context
//     window (🎯T926) — terminal for that seat, never a hard-block
//   - unknown: classified as failure but not mapped
//   - none: empty / not a failure signal (including busy)
type Class string

const (
	ClassNone               Class = ""
	ClassBackendUnavailable Class = "backend_unavailable"
	ClassRateLimit          Class = "rate_limit"
	ClassSpendWall          Class = "spend_wall"
	ClassAccountWall        Class = "account_wall"
	ClassAuth               Class = "auth"
	ClassClientBug          Class = "client_bug"
	ClassUnknown            Class = "unknown"
)

// String returns the stable code (or "none" when empty).
func (c Class) String() string {
	if c == ClassNone {
		return "none"
	}
	return string(c)
}

// IsTransient reports whether this class warrants poll/retry/re-pressure
// (method form for T236 consumers; same as TransientBackend).
func (c Class) IsTransient() bool {
	return TransientBackend(c)
}

// IsFailure reports whether this is a non-empty failure class
// (method form for T236 consumers; same as IsFailure function).
func (c Class) IsFailure() bool {
	return c != ClassNone
}

// Classify maps an error to a Class. Nil, a typed busy cause, and an error
// with no provider-failure cause are ClassNone. A rendered sentence is not
// a class. Auth, rate-limit, spend, account, backend, client-bug,
// retry-exhausted, and context-overflow come from the cause.
func Classify(err error) Class {
	if err == nil {
		return ClassNone
	}
	switch cause := ProviderCause(err); cause {
	case "":
		return ClassNone
	case CauseTurnInFlight, CauseBusy, CauseProcessNotRunning, CauseSteerUnsupported, CauseTurnIdle:
		return ClassNone
	default:
		if class := ClassFromCause(cause); class != ClassNone {
			return class
		}
		return ClassUnknown
	}
}

// ClassFromCause maps an exact known provider-failure cause to a class.
// Any other string, including a sentence and an empty cause, is ClassNone.
// It does not read the rendered text.
func ClassFromCause(cause string) Class {
	switch cause {
	case CauseAuth:
		return ClassAuth
	case CauseRateLimit:
		return ClassRateLimit
	case CauseSpendWall:
		return ClassSpendWall
	case CauseAccountWall:
		return ClassAccountWall
	case CauseBackendUnavailable:
		return ClassBackendUnavailable
	case CauseClientBug:
		return ClassClientBug
	case CauseRetryExhausted:
		return ClassRetryExhausted
	case CauseContextOverflow:
		return ClassContextOverflow
	default:
		return ClassNone
	}
}

// ClassifyText reports the class of a rendered sentence. A sentence is not
// a provider-failure cause, so this is ClassNone. Retry-exhausted and
// context-overflow are causes read by Classify.
func ClassifyText(string) Class {
	return ClassNone
}

// ClassifyMessage is an alias of ClassifyText (T236 import name).
func ClassifyMessage(msg string) Class {
	return ClassifyText(msg)
}

// TransientBackend reports classes that warrant poll/retry/re-pressure once
// the provider may have recovered (🎯T236 / T237). Local client bugs and
// auth failures fail closed — do not busy-loop as if the cloud were recovering.
// ClassUnknown is re-pressure-worthy residual (T236).
func TransientBackend(c Class) bool {
	switch c {
	case ClassBackendUnavailable, ClassRateLimit, ClassUnknown:
		return true
	case ClassRetryExhausted:
		// Not transient in the poll-and-hope sense: the CLI already spent
		// its own retry budget on this step and gave up. Re-pressuring the
		// identical step again is what 🎯T862.12 exists to name rather than
		// hide — the turn needs a fresh attempt (new turn), not a blind
		// retry of one already declared exhausted.
		return false
	default:
		return false
	}
}

// IsFailure is true for any non-empty classified failure (not ClassNone).
func IsFailure(c Class) bool {
	return c != ClassNone
}

// OwnerCopy returns actionable owner-visible text for a classified failure.
// Never returns bare "Internal error" alone when class is known.
func OwnerCopy(class Class, raw string) string {
	raw = strings.TrimSpace(raw)
	switch class {
	case ClassBackendUnavailable:
		return "Provider backend unavailable (backend_unavailable). The cloud side looks down or unreachable — waiting/retrying rather than a local config bug. " +
			detailSuffix(raw)
	case ClassRateLimit:
		return "Provider rate limit (rate_limit). Backing off; will retry when capacity allows. " +
			detailSuffix(raw)
	case ClassSpendWall:
		return "Provider spend limit (spend_wall). This will not recover by waiting. " +
			detailSuffix(raw)
	case ClassAccountWall:
		return "Provider account blocked (account_wall). This will not recover by waiting. " +
			detailSuffix(raw)
	case ClassAuth:
		return "Provider authentication failed (auth). Check Grok/Claude/Codex sign-in or API keys — this will not recover by waiting. " +
			detailSuffix(raw)
	case ClassClientBug:
		return "Local client/session error (client_bug). Fix config, session, or wire state; not a cloud outage. " +
			detailSuffix(raw)
	case ClassRetryExhausted:
		return "Provider retry budget exhausted (retry_exhausted). The provider/harness retry loop gave up; the turn ended without finishing. Retry budgets are provider-specific, not configured by Jevons. Start a new turn when the underlying failure is resolved. " + detailSuffix(raw)
	case ClassContextOverflow:
		return contextOverflowCopy(raw)
	case ClassUnknown:
		return "Provider failure (unknown). Class not pinned from the error string; see detail. " +
			detailSuffix(raw)
	default:
		return raw
	}
}

// FormatOwnerError combines class + owner copy for API/error frames.
func FormatOwnerError(class Class, raw string) string {
	if !IsFailure(class) {
		if raw != "" {
			return raw
		}
		return "unknown error"
	}
	return OwnerCopy(class, raw)
}

// ClassifyAndFormat is the one-shot for send paths: class + owner-visible message.
func ClassifyAndFormat(err error) (Class, string) {
	if err == nil {
		return ClassNone, ""
	}
	class := Classify(err)
	if !IsFailure(class) {
		return class, err.Error()
	}
	return class, FormatOwnerError(class, err.Error())
}

// FailureFields returns structured map fields for slog / eventlog (T236 consumers).
func FailureFields(class Class, raw string, extra map[string]any) map[string]any {
	out := map[string]any{
		"failure_class": class.String(),
		"transient":     TransientBackend(class),
	}
	if raw != "" {
		out["raw"] = truncate(raw, 400)
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// AnnotatedError wraps err with class prefix for log lines.
func AnnotatedError(class Class, err error) error {
	if err == nil {
		return nil
	}
	if !IsFailure(class) {
		return err
	}
	return fmt.Errorf("%s: %w", class, err)
}

func detailSuffix(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	return "Detail: " + truncate(raw, 240)
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
