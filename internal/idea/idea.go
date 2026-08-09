// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package idea is durable owner-spark intake + triage (🎯T325.3).
// Sparks land in a listable ledger (not only main-chat scrollback).
// Full opportunity-cost optimisation and multi life-domain automation
// stay parked (org map §6–§7).
package idea

import (
	"fmt"
	"strings"
	"time"
)

// Disposition is the triage bucket after intake (org map §6.1).
type Disposition string

const (
	// Inbox — captured, not yet triaged.
	Inbox Disposition = "inbox"
	// File — product-shaped → file bullseye (+ T193 spawn if Build).
	File Disposition = "file"
	// Park — needs-owner / design-discussion / park-for-design.
	Park Disposition = "park"
	// Hold — life-domain parked domain; no implementer (map §7).
	Hold Disposition = "hold"
	// Drop — rare one-off noise (prefer park with reason).
	Drop Disposition = "drop"
)

// Source is how the spark entered the ledger.
type Source string

const (
	SourceCapture Source = "capture" // capture: prefix
	SourceIdea    Source = "idea"    // idea: prefix
	SourceAside   Source = "aside"   // freeform aside:
	SourceChat    Source = "chat"    // main chat / overseer ceremony
	SourceMCP     Source = "mcp"     // jevons_idea_capture
	SourceAPI     Source = "api"     // POST /api/ideas without tagged source
)

// Record is one durable spark.
type Record struct {
	ID          string      `json:"id"`
	Text        string      `json:"text"`
	Title       string      `json:"title,omitempty"`
	Source      Source      `json:"source"`
	Disposition Disposition `json:"disposition"`
	// Domain is optional life/product domain tag (e.g. health, finance, jevons).
	Domain string `json:"domain,omitempty"`
	// Note carries triage reason / opportunity-cost scratch.
	Note string `json:"note,omitempty"`
	// TargetID when disposition=file and a 🎯 was allocated.
	TargetID string `json:"target_id,omitempty"`
	// AsideID when dual-written to a fleet purpose=aside (capture/aside).
	AsideID   string `json:"aside_id,omitempty"`
	CreatedAt string `json:"created_at"` // RFC3339 UTC
	CreatedUnix int64 `json:"created_unix"`
	// TriagedAt set when leaving inbox (RFC3339 UTC); empty while inbox.
	TriagedAt string `json:"triaged_at,omitempty"`
}

// CaptureArgs is the intake request (no functional options).
type CaptureArgs struct {
	Text    string
	Source  Source
	Domain  string
	AsideID string
	// Now overrides wall clock in tests; zero = time.Now().UTC().
	Now time.Time
	// ID overrides auto-mint in tests; empty = idea-<unix>-<rand-ish>.
	ID string
}

// TriageArgs updates disposition after capture.
type TriageArgs struct {
	ID          string
	Disposition Disposition
	Note        string
	Domain      string
	TargetID    string
	// Now overrides wall clock in tests.
	Now time.Time
}

// NormalizeDisposition maps free text to a known bucket; unknown → inbox.
func NormalizeDisposition(d string) Disposition {
	switch strings.ToLower(strings.TrimSpace(d)) {
	case string(File), "file-bullseye", "product", "product-shaped":
		return File
	case string(Park), "park-for-design", "design", "needs-owner", "design-discussion":
		return Park
	case string(Hold), "life-hold", "life-domain":
		return Hold
	case string(Drop), "ignore", "noise":
		return Drop
	case string(Inbox), "":
		return Inbox
	default:
		return Inbox
	}
}

// NormalizeSource maps free text to a known source; empty/unknown → api.
func NormalizeSource(s string) Source {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(SourceCapture):
		return SourceCapture
	case string(SourceIdea):
		return SourceIdea
	case string(SourceAside), "side":
		return SourceAside
	case string(SourceChat), "main", "overseer":
		return SourceChat
	case string(SourceMCP):
		return SourceMCP
	case string(SourceAPI), "":
		return SourceAPI
	default:
		return SourceAPI
	}
}

// ParkedLifeDomains are org-map §7 domains that stay hold — no implementers
// until the owner unparks. Case-insensitive match via IsParkedLifeDomain.
func ParkedLifeDomains() []string {
	return []string{
		"swot",
		"life-domain",
		"life-domains",
		"education",
		"finance",
		"health",
		"leisure",
		"entertainment",
		"device-life-app",
		"life-app",
		"hardware",
		"cat-flap",
		"factory-parity",
		"t254",
	}
}

// IsParkedLifeDomain reports whether domain is in the §7 park set.
func IsParkedLifeDomain(domain string) bool {
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" {
		return false
	}
	for _, p := range ParkedLifeDomains() {
		if d == p || strings.Contains(d, p) {
			return true
		}
	}
	return false
}

// NextCeremony is the thin product ceremony for a disposition (documented path).
func NextCeremony(d Disposition) string {
	switch NormalizeDisposition(string(d)) {
	case File:
		return "product-shaped → file bullseye (jevons_target_file / target:) and T193 spawn if Build-plane"
	case Park:
		return "needs-owner/design → park-for-design or design-discussion target; no unattended implementer"
	case Hold:
		return "life-domain parked (§7) → hold queue; capture ok; no implementer until owner unparks"
	case Drop:
		return "one-off noise → drop with reason; prefer park when unsure"
	default:
		return "inbox → triage to file | park | hold | drop (root or light staff cycle)"
	}
}

// SuggestDisposition picks a thin default from domain + free text.
// Explicit product keywords → file; parked life domain → hold; else inbox
// for human triage. Does not auto-drop.
func SuggestDisposition(text, domain string) Disposition {
	if IsParkedLifeDomain(domain) {
		return Hold
	}
	lower := strings.ToLower(text + " " + domain)
	for _, p := range ParkedLifeDomains() {
		if p == "t254" || p == "factory-parity" {
			continue // too easy to false-positive on product text
		}
		if strings.Contains(lower, p) && (p == "swot" || p == "cat-flap" || p == "life-app" || p == "device-life-app") {
			return Hold
		}
	}
	// Light product heuristic — still leaves most sparks in inbox.
	productHints := []string{"bug", "fix", "target:", "bullseye", "fleet", "daemon", "oracle", "hermetic", "journey"}
	for _, h := range productHints {
		if strings.Contains(lower, h) {
			return File
		}
	}
	return Inbox
}

// TitleFromBody makes a short list label (first line / 72 runes).
func TitleFromBody(body string) string {
	t := strings.TrimSpace(body)
	if t == "" {
		return ""
	}
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	// rune-safe truncate
	runes := []rune(t)
	const max = 72
	if len(runes) > max {
		return string(runes[:max-1]) + "…"
	}
	return t
}

// NewRecord builds a validated Record from CaptureArgs (pure; no I/O).
func NewRecord(args CaptureArgs) (Record, error) {
	text := strings.TrimSpace(args.Text)
	if text == "" {
		return Record{}, fmt.Errorf("idea: text is required")
	}
	now := args.Now
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	id := strings.TrimSpace(args.ID)
	if id == "" {
		id = fmt.Sprintf("idea-%d", now.UnixNano())
	}
	src := NormalizeSource(string(args.Source))
	domain := strings.TrimSpace(args.Domain)
	disp := Inbox
	if IsParkedLifeDomain(domain) {
		disp = Hold
	}
	return Record{
		ID:          id,
		Text:        text,
		Title:       TitleFromBody(text),
		Source:      src,
		Disposition: disp,
		Domain:      domain,
		AsideID:     strings.TrimSpace(args.AsideID),
		CreatedAt:   now.Format(time.RFC3339),
		CreatedUnix: now.Unix(),
	}, nil
}

// ApplyTriage returns a copy of rec with disposition fields updated (pure).
func ApplyTriage(rec Record, args TriageArgs) (Record, error) {
	if strings.TrimSpace(args.ID) != "" && args.ID != rec.ID {
		return Record{}, fmt.Errorf("idea: id mismatch")
	}
	if err := validateDispositionInput(string(args.Disposition)); err != nil {
		return Record{}, err
	}
	d := NormalizeDisposition(string(args.Disposition))
	now := args.Now
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	out := rec
	out.Disposition = d
	if note := strings.TrimSpace(args.Note); note != "" {
		out.Note = note
	}
	if domain := strings.TrimSpace(args.Domain); domain != "" {
		out.Domain = domain
	}
	// Parked life domain only forces hold while still inbox; explicit
	// file/park/drop from root is the unpark / override path.
	if d == Inbox && IsParkedLifeDomain(out.Domain) {
		out.Disposition = Hold
		d = Hold
	}
	if tid := strings.TrimSpace(args.TargetID); tid != "" {
		out.TargetID = tid
	}
	if d != Inbox {
		out.TriagedAt = now.Format(time.RFC3339)
	} else {
		out.TriagedAt = ""
	}
	return out, nil
}

func validateDispositionInput(raw string) error {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return fmt.Errorf("idea: disposition is required")
	}
	known := map[string]bool{
		"inbox": true, "file": true, "park": true, "hold": true, "drop": true,
		"file-bullseye": true, "product": true, "product-shaped": true,
		"park-for-design": true, "design": true, "needs-owner": true, "design-discussion": true,
		"life-hold": true, "life-domain": true, "ignore": true, "noise": true,
	}
	if !known[s] {
		return fmt.Errorf("idea: unknown disposition %q", raw)
	}
	return nil
}
