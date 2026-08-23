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
//   - rate_limit: 429 / quota / throttle
//   - auth: 401 / 403 / API key / unauthorized
//   - client_bug: local config, wire, session, bad request
//   - unknown: classified as failure but not mapped
//   - none: empty / not a failure signal (including busy)
type Class string

const (
	ClassNone               Class = ""
	ClassBackendUnavailable Class = "backend_unavailable"
	ClassRateLimit          Class = "rate_limit"
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

// Classify maps an error to a Class. Nil and busy → ClassNone.
func Classify(err error) Class {
	if err == nil {
		return ClassNone
	}
	if IsPromptBusy(err) {
		return ClassNone
	}
	return ClassifyText(err.Error())
}

// ClassifyText maps provider/ACP failure text or owner-visible copy to Class.
// Hermetic fixture surface for 🎯T237; T236 consumes the result only.
// Prefer structured codes when available; string patterns are a residual
// for wire messages that only carry prose today (e.g. Grok "Internal error").
func ClassifyText(msg string) Class {
	s := strings.TrimSpace(msg)
	if s == "" {
		return ClassNone
	}
	low := strings.ToLower(s)

	// Busy is not a provider failure.
	if isBusyMessage(low) {
		return ClassNone
	}

	// Auth first — "unauthorized" before generic "error". Account/key walls
	// that Classify would otherwise miss (revoked, suspended) also land here
	// so 🎯T406 HardBlock sees ClassAuth rather than ClassNone.
	if containsAny(low, authMarkers...) || isAccountWall(low) {
		return ClassAuth
	}

	// Rate limit — including account-level spend/billing walls (🎯T406).
	// ClassifyText maps spend walls to rate_limit so 🎯T407's auth/rate_limit
	// cluster still sees them; HardBlock then distinguishes spend from a
	// transient 429.
	if containsAny(low, rateLimitMarkers...) || isSpendWall(low) {
		return ClassRateLimit
	}

	// Client / local bugs — fail closed, do not treat as cloud recovering.
	// Exclude backend signatures that share the word "error".
	if containsAny(low, clientBugMarkers...) {
		if !isBackendSignature(low) {
			return ClassClientBug
		}
	}

	// Backend / transient outage (includes bare "Internal error").
	if isBackendSignature(low) || containsAny(low, backendUnavailableMarkers...) {
		return ClassBackendUnavailable
	}

	// Residual failure-shaped text without a clear class.
	if containsAny(low, "error", "failed", "failure", "exception") {
		return ClassUnknown
	}
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
	case ClassAuth:
		return "Provider authentication failed (auth). Check Grok/Claude/Codex sign-in or API keys — this will not recover by waiting. " +
			detailSuffix(raw)
	case ClassClientBug:
		return "Local client/session error (client_bug). Fix config, session, or wire state; not a cloud outage. " +
			detailSuffix(raw)
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
	if isBareInternalError(strings.ToLower(raw)) {
		return "(provider returned Internal error)"
	}
	return "Detail: " + truncate(raw, 240)
}

func isBackendSignature(low string) bool {
	if isBareInternalError(low) {
		return true
	}
	return containsAny(low,
		"internal error", "internal server error",
		"service unavailable", "backend unavailable", "backend down",
		"provider unavailable", "provider down",
	)
}

func isBareInternalError(lower string) bool {
	s := strings.Trim(lower, " \t\n\r.!,:;")
	switch s {
	case "internal error", "internal", "an internal error", "an internal error occurred",
		"internal server error", "error: internal error",
		"acp session/prompt: internal error", "acp session/prompt: internal":
		return true
	}
	if strings.HasSuffix(s, ": internal error") || strings.HasSuffix(s, ": internal") {
		return true
	}
	// Whole-message Internal error with only short noise around it.
	if strings.Contains(s, "internal error") && len(s) <= 48 {
		return true
	}
	return false
}

func isBusyMessage(lower string) bool {
	return containsAny(lower,
		"already in flight",
		" is busy",
		"prompt in progress",
		"session busy",
		"turn in progress",
	)
}

func containsAny(low string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(low, n) {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// Marker lists — representative strings/codes from Grok ACP, Claude, Codex,
// and common HTTP/transport failures. Live residual: real outage copy may
// evolve; hermetic fixtures lock these (🎯T237).

var authMarkers = []string{
	"unauthorized",
	"unauthorised",
	"authentication",
	"not authenticated",
	"invalid api key",
	"invalid_api_key",
	"api key",
	"apikey",
	"not signed in",
	"login required",
	"forbidden",
	"access denied",
	"401",
	"403",
	"permission denied",
	"auth failed",
	"auth error",
	"credentials",
}

var rateLimitMarkers = []string{
	"rate limit",
	"rate_limit",
	"ratelimit",
	"too many requests",
	"429",
	"quota exceeded",
	"quota_exceeded",
	"quota limit",
	"capacity",
	"throttl",
	"resource_exhausted",
	"requests per minute",
	"rpm limit",
}

var clientBugMarkers = []string{
	"no session",
	"session not found",
	"client closed",
	"no transport",
	"unknown session",
	"refusing to mint",
	"malformed",
	"invalid json",
	"json decode",
	"decode failed",
	"not registered",
	"name and text are required",
	"text is required",
	"name is required",
	"unsupported capability",
	"unknown agent provider",
	"invalid request",
	"bad request",
	"400",
	"unknown method",
	"unsupported",
	"invalid argument",
	"config error",
	"configuration",
	"parse error",
	"method not allowed",
	"no registry",
	"agent registry not available",
	"session/load",
	"existing conversation; refusing",
}

var backendUnavailableMarkers = []string{
	"service unavailable",
	"backend unavailable",
	"backend down",
	"provider down",
	"connection refused",
	"connection reset",
	"connection timed out",
	"i/o timeout",
	"i/o error",
	"network is unreachable",
	"network error",
	"no such host",
	"temporarily unavailable",
	"try again",
	"timed out",
	"timeout",
	"bad gateway",
	"gateway timeout",
	"500",
	"503",
	"502",
	"504",
	"status 503",
	"status 502",
	"status 504",
	"econnrefused",
	"econnreset",
	"enotfound",
	"server error",
	"overloaded",
	"over capacity",
	"upstream",
	"cloudflare",
	"dial tcp",
	"ws: close",
	"websocket: close",
	"connection closed waiting",
	"eof",
}
