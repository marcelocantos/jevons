// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package secauditor is the standing security interest for fleet confinement
// (🎯T335). It detects deny/drift/bypass/secret/high-risk patterns and
// surfaces judgments to the overseer. It does not implement product features;
// control-plane acts are bounded (halt spawn, kill worker, file target) via
// injected interfaces.
package secauditor

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/writconf"
)

// DefaultName is the durable auditor identity handle.
const DefaultName = "security-auditor"

// EventSource stamps overseer-directed security deliveries.
const EventSource = "security-auditor"

// Component is the eventlog component for dual-write filters.
const Component = "security_auditor"

// AlertDeliverer posts a formatted alert to the overseer (fire-and-forget).
// Production: Deliver / notify queue. Never a bullseye filer by itself.
type AlertDeliverer interface {
	DeliverAlert(text string) error
}

// ControlPlane is the only set of side-effects the auditor may request.
// Implementors live in the product control plane (MCP/server); the auditor
// package stays pure aside from calling these hooks.
type ControlPlane interface {
	// HaltSpawn freezes new worker spawns (budget/spawn gate style).
	HaltSpawn(reason string) error
	// KillWorker stops a named fleet worker.
	KillWorker(name, reason string) error
	// FileTarget records a standing product gap (optional; may be no-op).
	FileTarget(name, acceptance string) error
}

// ControlAct is a bounded control-plane recommendation or action taken.
type ControlAct string

const (
	ActNone       ControlAct = "none"
	ActHaltSpawn  ControlAct = "halt_spawn"
	ActKillWorker ControlAct = "kill_worker"
	ActFileTarget ControlAct = "file_target"
)

// Alert is one security judgment for the overseer.
type Alert struct {
	// Severity: low | medium | high | critical
	Severity string
	Kind     writconf.EventKind
	Message  string
	Event    writconf.Event
	// ControlAct is the recommended or executed bounded act.
	ControlAct ControlAct
	// Fingerprint dedupes repeated alerts.
	Fingerprint string
	At          time.Time
}

// EventLogger dual-writes security decisions (matches eventlog.LogEvent shape).
type EventLogger func(level, component, decision, msg string, fields map[string]any)

// Interest is the standing security auditor.
type Interest struct {
	mu        sync.Mutex
	recent    []Alert
	delivered map[string]time.Time // fingerprint → last deliver
	// MaxRecent caps the in-memory ring for /api/security/status.
	MaxRecent int
	// DedupWindow suppresses identical fingerprints.
	DedupWindow time.Duration

	Deliverer AlertDeliverer
	Control   ControlPlane
	Log       EventLogger
}

// New returns an Interest with sensible defaults.
func New() *Interest {
	return &Interest{
		MaxRecent:   50,
		DedupWindow: 10 * time.Minute,
		delivered:   map[string]time.Time{},
	}
}

// Observe classifies an event, optionally acts on the control plane, logs,
// and delivers a judgment to the overseer. Returns the alert (even if
// delivery was suppressed by dedup).
func (a *Interest) Observe(ev writconf.Event) Alert {
	alert := Classify(ev)
	if a == nil {
		return alert
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.delivered == nil {
		a.delivered = map[string]time.Time{}
	}
	now := time.Now().UTC()
	alert.At = now

	// Bounded control-plane acts for high severity.
	if a.Control != nil {
		switch alert.ControlAct {
		case ActHaltSpawn:
			_ = a.Control.HaltSpawn(alert.Message)
		case ActKillWorker:
			if ev.Agent != "" {
				_ = a.Control.KillWorker(ev.Agent, alert.Message)
			}
		case ActFileTarget:
			_ = a.Control.FileTarget(
				"Security confinement residual: "+string(ev.Kind),
				alert.Message,
			)
		}
	}

	if a.Log != nil {
		a.Log("warn", Component, string(ev.Kind), alert.Message, map[string]any{
			"agent":          ev.Agent,
			"host":           ev.Host,
			"path":           ev.Path,
			"manifest_field": ev.ManifestField,
			"fingerprint":    alert.Fingerprint,
			"control_act":    string(alert.ControlAct),
			"severity":       alert.Severity,
			"outcome":        "alert",
		})
	}

	// Ring buffer of recent alerts.
	max := a.MaxRecent
	if max <= 0 {
		max = 50
	}
	a.recent = append(a.recent, alert)
	if len(a.recent) > max {
		a.recent = a.recent[len(a.recent)-max:]
	}

	// Dedup deliver.
	window := a.DedupWindow
	if window <= 0 {
		window = 10 * time.Minute
	}
	if last, ok := a.delivered[alert.Fingerprint]; ok && now.Sub(last) < window {
		return alert
	}
	a.delivered[alert.Fingerprint] = now

	if a.Deliverer != nil {
		_ = a.Deliverer.DeliverAlert(FormatOverseerAlert(alert))
	}
	return alert
}

// Recent returns a copy of recent alerts (newest last).
func (a *Interest) Recent() []Alert {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Alert, len(a.recent))
	copy(out, a.recent)
	return out
}

// Status is a probe-friendly snapshot for GET /api/security/status (T194).
type Status struct {
	Auditor       string `json:"auditor"`
	Ready         bool   `json:"ready"`
	RecentAlerts  int    `json:"recent_alerts"`
	Confinement   string `json:"confinement"`
	WritAvailable bool   `json:"writ_available"`
	WritBin       string `json:"writ_bin,omitempty"`
}

// Classify pure-maps a confinement event to an alert + recommended act.
func Classify(ev writconf.Event) Alert {
	sev := "medium"
	act := ActNone
	msg := strings.TrimSpace(ev.Message)
	if msg == "" {
		msg = string(ev.Kind)
	}
	switch ev.Kind {
	case writconf.KindDenyNet, writconf.KindDenyFS:
		sev = "high"
		msg = fmt.Sprintf("confinement denied undeclared access (%s)", ev.Kind)
		if ev.Host != "" {
			msg += " host=" + ev.Host
		}
		if ev.Path != "" {
			msg += " path=" + ev.Path
		}
		// Repeated deny while agent still running → consider kill (recommendation).
		if ev.Agent != "" {
			act = ActKillWorker
		}
	case writconf.KindDrift:
		sev = "high"
		msg = "writ drift (declared-vs-actual): " + msg
		act = ActFileTarget
	case writconf.KindBypass:
		sev = "critical"
		msg = "confinement bypass attempt: " + msg
		act = ActHaltSpawn
	case writconf.KindSecretPath:
		sev = "critical"
		msg = "secret-path access: " + first(ev.Path, msg)
		act = ActKillWorker
	case writconf.KindHighRiskEgress:
		sev = "critical"
		msg = "high-risk egress: host=" + ev.Host
		act = ActHaltSpawn
	case writconf.KindPolicyDeny:
		sev = "medium"
		msg = "policy denied high-risk spawn: " + msg
	default:
		if msg == string(ev.Kind) || msg == "" {
			msg = "security observation: " + string(ev.Kind)
		}
	}
	fp := fingerprint(ev)
	return Alert{
		Severity:    sev,
		Kind:        ev.Kind,
		Message:     msg,
		Event:       ev,
		ControlAct:  act,
		Fingerprint: fp,
	}
}

// FormatOverseerAlert builds the wire text for the overseer.
func FormatOverseerAlert(a Alert) string {
	var b strings.Builder
	b.WriteString("[security-auditor] ")
	b.WriteString(strings.ToUpper(a.Severity))
	b.WriteString(" ")
	b.WriteString(string(a.Kind))
	b.WriteString(": ")
	b.WriteString(a.Message)
	if a.Event.Agent != "" {
		b.WriteString("\nagent: ")
		b.WriteString(a.Event.Agent)
	}
	if a.Event.Host != "" {
		b.WriteString("\nhost: ")
		b.WriteString(a.Event.Host)
	}
	if a.Event.Path != "" {
		b.WriteString("\npath: ")
		b.WriteString(a.Event.Path)
	}
	if a.Event.ManifestField != "" {
		b.WriteString("\nmanifest_field: ")
		b.WriteString(a.Event.ManifestField)
	}
	if a.ControlAct != ActNone {
		b.WriteString("\ncontrol_act: ")
		b.WriteString(string(a.ControlAct))
	}
	b.WriteString("\n(security auditor: judgments only; bounded control-plane acts; no product implementation)")
	return b.String()
}

func fingerprint(ev writconf.Event) string {
	return strings.Join([]string{
		string(ev.Kind),
		ev.Agent,
		ev.Host,
		ev.Path,
		ev.ManifestField,
	}, "|")
}

func first(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
