// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package planusage

import (
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/seatprovider"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

// BurnProfile describes the fraction of a window's spend on each UTC weekday
// (Sunday=0). Windows is the count of sufficiently observed completed windows.
// A fallback explicitly says that the diagonal, rather than a learned curve, applies.
type BurnProfile struct {
	Shares   [7]float64 `json:"weekday_shares"`
	Windows  int        `json:"windows"`
	Fallback bool       `json:"fallback"`
}

type burnSample struct {
	at, reset               time.Time
	remaining               float64
	provider, window, model string
	canonical               bool
}

// MineBurnProfiles reads the stored observations, not the browser's truncated
// current-window sparkline. It never polls a vendor. A failed read is an
// explicit fallback rather than a plausible-looking invented weekday pattern.
func MineBurnProfiles(replica Replica) map[string]BurnProfile {
	result := map[string]BurnProfile{}
	if replica == nil {
		return result
	}
	var samples []burnSample
	err := replica.Read(func(db *sqlpipe.Database) error {
		// Claudia's canonical history, including previous periods. Restrict to
		// day-scale windows; a five-hour session has no meaningful daily shape.
		q, err := db.Query(`SELECT h.provider, h.observed_at, json_extract(w.value,'$.name'),
   COALESCE(json_extract(w.value,'$.model'),''), json_extract(w.value,'$.resets_at'),
   json_extract(w.value,'$.remaining_percent'), json_extract(w.value,'$.limit_window')
   FROM claudia_plan_usage_history h JOIN json_each(h.reading_json,'$.windows') w
   WHERE json_extract(w.value,'$.remaining_percent') IS NOT NULL
    AND json_extract(w.value,'$.resets_at') IS NOT NULL`)
		if err != nil {
			return err
		}
		for _, row := range q.Rows {
			provider, a := row[0].(string)
			at, b := row[1].(string)
			name, c := row[2].(string)
			model, d := row[3].(string)
			reset, e := row[4].(string)
			rem, f := burnNumber(row[5])
			limit, _ := burnNumber(row[6])
			if !(a && b && c && d && e && f) {
				continue
			}
			t, te := time.Parse(time.RFC3339Nano, at)
			r, re := time.Parse(time.RFC3339Nano, reset)
			if te != nil || re != nil {
				continue
			}
			samples = append(samples, burnSample{at: t, reset: r, remaining: rem, provider: provider, window: burnWindowName(provider, name, model, limit), model: model, canonical: true})
		}
		// Imported pre-replica readings remain useful only for periods not
		// represented by canonical history (no double counting).
		q, err = db.Query(`SELECT provider,window,fetched_at,resets_key,remaining FROM jv_plan_history_legacy WHERE resets_key<>''`)
		if err != nil {
			return err
		}
		for _, row := range q.Rows {
			p, a := row[0].(string)
			w, b := row[1].(string)
			at, c := row[2].(string)
			reset, d := row[3].(string)
			rem, e := burnNumber(row[4])
			if !(a && b && c && d && e) {
				continue
			}
			t, te := time.Parse(time.RFC3339Nano, at)
			r, re := time.Parse(time.RFC3339Nano, reset)
			if te != nil || re != nil {
				continue
			}
			samples = append(samples, burnSample{at: t, reset: r, remaining: rem, provider: p, window: w})
		}
		return nil
	})
	if err != nil {
		return result
	}
	return calculateBurnProfiles(samples, time.Now())
}

func burnNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n)
	case int64:
		return float64(n), true
	case json.Number:
		x, e := n.Float64()
		return x, e == nil
	}
	return 0, false
}
func burnWindowName(provider, name, model string, limitNanos float64) string {
	isCursor := seatprovider.Resolve(claudia.Provider(provider)).Plan == seatprovider.Cursor
	if isCursor && model == "api" {
		return "api"
	}
	if name == "weekly" && (isCursor || limitNanos/1e9 >= 25*86400) {
		return "monthly"
	}
	return name
}
func burnKey(provider, window string) string { return provider + "/" + window }
func windowDuration(name string) time.Duration {
	switch name {
	case "weekly", "weekly_model":
		return 7 * 24 * time.Hour
	case "monthly":
		return 30 * 24 * time.Hour
	}
	return 0
}
func calculateBurnProfiles(samples []burnSample, now time.Time) map[string]BurnProfile {
	type period struct {
		key       string
		canonical bool
		points    []burnSample
	}
	groups := map[string]*period{}
	for _, s := range samples {
		duration := windowDuration(s.window)
		if duration == 0 || s.reset.After(now) || s.reset.Before(s.at) || s.at.Before(s.reset.Add(-duration-10*time.Minute)) {
			continue
		}
		// Reset timestamps jitter by seconds; collapse to a ten-minute bucket.
		id := burnKey(s.provider, s.window) + "/" + s.model + "/" + s.reset.Round(ResetsKeyBucket).Format(time.RFC3339)
		g := groups[id]
		if g == nil {
			g = &period{key: burnKey(s.provider, s.window)}
			groups[id] = g
		}
		if s.canonical && !g.canonical {
			g.canonical = true
			g.points = nil
		}
		if g.canonical && !s.canonical {
			continue
		}
		g.points = append(g.points, s)
	}
	totals := map[string][7]float64{}
	counts := map[string]int{}
	for _, g := range groups {
		pts := g.points
		if len(pts) < 2 {
			continue
		}
		sort.Slice(pts, func(i, j int) bool { return pts[i].at.Before(pts[j].at) })
		duration := windowDuration(pts[0].window)
		start := pts[0].reset.Add(-duration)
		// Both edges must be observed within one day: sparse/partial periods
		// otherwise manufacture a weekday preference from absent observations.
		if pts[0].at.Sub(start) > 24*time.Hour || pts[0].reset.Sub(pts[len(pts)-1].at) > 24*time.Hour {
			continue
		}
		var daily [7]float64
		for i := 1; i < len(pts); i++ {
			prev, cur := pts[i-1], pts[i]
			if !cur.at.After(prev.at) {
				continue
			}
			delta := prev.remaining - cur.remaining
			// Reset artefacts and replenishments do not count as spend.
			if delta <= 0 || delta > 100 {
				continue
			}
			from := prev.at
			for from.Before(cur.at) {
				midnight := from.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
				to := cur.at
				if midnight.Before(to) {
					to = midnight
				}
				daily[int(from.UTC().Weekday())] += delta * to.Sub(from).Seconds() / cur.at.Sub(prev.at).Seconds()
				from = to
			}
		}
		sum := 0.0
		for _, v := range daily {
			sum += v
		}
		if sum < 5 {
			continue
		}
		normalized := totals[g.key]
		for i, v := range daily {
			normalized[i] += v / sum
		}
		totals[g.key] = normalized
		counts[g.key]++
	}
	out := map[string]BurnProfile{}
	for key, count := range counts {
		p := BurnProfile{Windows: count, Fallback: count < 3}
		if !p.Fallback {
			for i, v := range totals[key] {
				p.Shares[i] = v / float64(count)
			}
		}
		out[key] = p
	}
	return out
}

// AttachBurnProfiles projects one backend/window-kind profile onto each
// current window, including an explicit fallback for thin history.
func AttachBurnProfiles(snap Snapshot, profiles map[string]BurnProfile) Snapshot {
	for i := range snap.Backends {
		for j := range snap.Backends[i].Windows {
			w := &snap.Backends[i].Windows[j]
			p, ok := profiles[burnKey(snap.Backends[i].Provider, w.Name)]
			if !ok {
				p = BurnProfile{Fallback: true}
			}
			w.BurnProfile = &p
		}
	}
	return snap
}

// WithBurnProfilesJSON preserves the projection's opaque fields (including
// transport timestamps unknown to Snapshot) while adding mined profiles.
func WithBurnProfilesJSON(raw json.RawMessage, profiles map[string]BurnProfile) json.RawMessage {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	backends, ok := obj["backends"].([]any)
	if !ok {
		return raw
	}
	for _, b := range backends {
		backend, ok := b.(map[string]any)
		if !ok {
			continue
		}
		provider, _ := backend["provider"].(string)
		windows, _ := backend["windows"].([]any)
		for _, w := range windows {
			window, ok := w.(map[string]any)
			if !ok {
				continue
			}
			name, _ := window["name"].(string)
			p, ok := profiles[burnKey(provider, name)]
			if !ok {
				p = BurnProfile{Fallback: true}
			}
			window["burn_profile"] = p
		}
	}
	encoded, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return encoded
}
