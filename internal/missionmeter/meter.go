// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package missionmeter computes offline proxies from the dated sidecar spool
// and the durable lifecycle journal. Bytes are serialized message JSON bytes,
// NOT token usage, context-window utilization or billing.
package missionmeter

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

// Window includes From and excludes To. Zero endpoints are unbounded.
type Window struct{ From, To time.Time }

func (w Window) contains(s string) (bool, error) {
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil {
		return false, e
	}
	return (w.From.IsZero() || !t.Before(w.From)) && (w.To.IsZero() || t.Before(w.To)), nil
}

type Seat struct {
	Name            string `json:"seat"`
	Tier            string `json:"tier"`
	TargetID        string `json:"target_id,omitempty"`
	Turns           int    `json:"turns"`
	MessageBytes    int64  `json:"message_bytes"`
	MaxMessageBytes int64  `json:"max_message_bytes"`
	Starts          int    `json:"starts"`
}
type Target struct {
	ID              string `json:"target_id"`
	Seats           int    `json:"seats"`
	MessageBytes    int64  `json:"message_bytes"`
	MaxMessageBytes int64  `json:"max_message_bytes"`
	Starts          int    `json:"starts"`
}
type Stat struct {
	Count int     `json:"count"`
	P90   float64 `json:"p90"`
	P99   float64 `json:"p99"`
	Mean  float64 `json:"mean"`
	Sigma float64 `json:"sigma"`
}
type Ranked struct {
	Seat  string  `json:"seat"`
	Value int64   `json:"value"`
	Rank  int     `json:"rank"`
	Sigma float64 `json:"sigma"`
}
type Metric struct {
	Distribution Stat     `json:"distribution"`
	Ranking      []Ranked `json:"ranking"`
}
type Report struct {
	Note           string   `json:"note"`
	Seats          []Seat   `json:"seats"`
	Targets        []Target `json:"targets"`
	WorkerSum      Metric   `json:"worker_message_bytes"`
	WorkerMax      Metric   `json:"worker_max_message_bytes"`
	WorkerStarts   Metric   `json:"worker_starts"`
	UnscopedStarts int      `json:"unscoped_starts"`
}

func tier(s string) string {
	if s == "jevons" || s == "overseer" {
		return "overseer"
	}
	if strings.HasSuffix(s, "-po") {
		return "po"
	}
	return "worker"
}

// Scan streams files one record at a time. A truncated final line, malformed
// JSON, invalid timestamp or oversized record is an error with path and line.
// No results should be used when Scan returns an error.
func Scan(spoolPaths, eventPaths []string, w Window) (Report, error) {
	seats := map[string]*Seat{}
	targets := map[string]string{}
	get := func(name string) *Seat {
		s := seats[name]
		if s == nil {
			s = &Seat{Name: name, Tier: tier(name)}
			seats[name] = s
		}
		return s
	}
	var unscoped int
	for _, p := range eventPaths {
		err := lines(p, func(b []byte) error {
			var e struct {
				TS        string `json:"ts"`
				Component string `json:"component"`
				Decision  string `json:"decision"`
				Fields    struct {
					Outcome  string `json:"outcome"`
					Name     string `json:"name"`
					TargetID string `json:"target_id"`
				} `json:"fields"`
			}
			if err := json.Unmarshal(b, &e); err != nil {
				return err
			}
			if e.Component != "agent_lifecycle" || e.Decision != "start" || e.Fields.Outcome != "ok" {
				return nil
			}
			if e.Fields.Name == "" {
				return fmt.Errorf("successful start missing name")
			}
			if e.Fields.TargetID != "" {
				targets[e.Fields.Name] = e.Fields.TargetID
			}
			yes, err := w.contains(e.TS)
			if err != nil {
				return fmt.Errorf("start timestamp: %w", err)
			}
			if !yes {
				return nil
			}
			s := get(e.Fields.Name)
			s.Starts++
			if e.Fields.TargetID == "" {
				unscoped++
			}
			return nil
		})
		if err != nil {
			return Report{}, err
		}
	}
	for _, p := range spoolPaths {
		err := lines(p, func(b []byte) error {
			// Decode only the required fields; snapshot is released after this line.
			var rec struct {
				TS       string `json:"ts"`
				Seat     string `json:"seat"`
				Type     string `json:"type"`
				Snapshot struct {
					Messages []json.RawMessage `json:"messages"`
				} `json:"snapshot"`
			}
			if err := json.Unmarshal(b, &rec); err != nil {
				return err
			}
			if rec.Type != "turn_end" {
				return nil
			}
			if rec.Seat == "" {
				return fmt.Errorf("turn_end missing seat")
			}
			yes, err := w.contains(rec.TS)
			if err != nil {
				return fmt.Errorf("turn_end timestamp: %w", err)
			}
			if !yes {
				return nil
			}
			s := get(rec.Seat)
			s.Turns++
			var turnBytes int64
			for _, msg := range rec.Snapshot.Messages {
				turnBytes += int64(len(bytes.TrimSpace(msg)))
			}
			s.MessageBytes += turnBytes
			if turnBytes > s.MaxMessageBytes {
				s.MaxMessageBytes = turnBytes
			}
			return nil
		})
		if err != nil {
			return Report{}, err
		}
	}
	out := Report{Note: "Serialized snapshot message JSON bytes are a repeated per-turn context proxy, not billed tokens or provider usage. Starts count successful lifecycle events, not turns; target attribution depends on lifecycle target_id.", UnscopedStarts: unscoped}
	byTarget := map[string]*Target{}
	for name, s := range seats {
		s.TargetID = targets[name]
		out.Seats = append(out.Seats, *s)
		if s.TargetID != "" {
			t := byTarget[s.TargetID]
			if t == nil {
				t = &Target{ID: s.TargetID}
				byTarget[s.TargetID] = t
			}
			t.Seats++
			t.MessageBytes += s.MessageBytes
			t.Starts += s.Starts
			if s.MaxMessageBytes > t.MaxMessageBytes {
				t.MaxMessageBytes = s.MaxMessageBytes
			}
		}
	}
	sort.Slice(out.Seats, func(i, j int) bool { return out.Seats[i].Name < out.Seats[j].Name })
	for _, t := range byTarget {
		out.Targets = append(out.Targets, *t)
	}
	sort.Slice(out.Targets, func(i, j int) bool { return out.Targets[i].ID < out.Targets[j].ID })
	out.WorkerSum = metric(out.Seats, func(s Seat) int64 { return s.MessageBytes })
	out.WorkerMax = metric(out.Seats, func(s Seat) int64 { return s.MaxMessageBytes })
	out.WorkerStarts = metric(out.Seats, func(s Seat) int64 { return int64(s.Starts) })
	return out, nil
}

func metric(seats []Seat, value func(Seat) int64) Metric {
	var m Metric
	for _, s := range seats {
		if s.Tier == "worker" {
			m.Ranking = append(m.Ranking, Ranked{Seat: s.Name, Value: value(s)})
		}
	}
	sort.Slice(m.Ranking, func(i, j int) bool {
		if m.Ranking[i].Value == m.Ranking[j].Value {
			return m.Ranking[i].Seat < m.Ranking[j].Seat
		}
		return m.Ranking[i].Value > m.Ranking[j].Value
	})
	n := len(m.Ranking)
	m.Distribution.Count = n
	if n == 0 {
		return m
	}
	vals := make([]float64, n)
	for i, r := range m.Ranking {
		vals[n-i-1] = float64(r.Value)
		m.Distribution.Mean += float64(r.Value) / float64(n)
	}
	m.Distribution.P90 = percentile(vals, .90)
	m.Distribution.P99 = percentile(vals, .99)
	for _, v := range vals {
		d := v - m.Distribution.Mean
		m.Distribution.Sigma += d * d / float64(n)
	}
	m.Distribution.Sigma = math.Sqrt(m.Distribution.Sigma)
	for i := range m.Ranking {
		m.Ranking[i].Rank = i + 1
		if m.Distribution.Sigma > 0 {
			m.Ranking[i].Sigma = (float64(m.Ranking[i].Value) - m.Distribution.Mean) / m.Distribution.Sigma
		}
	}
	return m
}
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	x := p * float64(len(sorted)-1)
	i := int(x)
	if i+1 == len(sorted) {
		return sorted[i]
	}
	return sorted[i] + (sorted[i+1]-sorted[i])*(x-float64(i))
}

func lines(path string, fn func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for line := 1; ; line++ {
		var b []byte
		for {
			part, err := r.ReadSlice('\n')
			if len(b)+len(part) > 16<<20 {
				return fmt.Errorf("%s:%d: record exceeds 16 MiB", path, line)
			}
			b = append(b, part...)
			if err == bufio.ErrBufferFull {
				continue
			}
			if err == io.EOF {
				if len(b) > 0 {
					return fmt.Errorf("%s:%d: truncated final line", path, line)
				}
				return nil
			}
			if err != nil {
				return fmt.Errorf("%s:%d: %w", path, line, err)
			}
			break
		}
		if len(bytes.TrimSpace(b)) == 0 {
			continue
		}
		if err := fn(b); err != nil {
			return fmt.Errorf("%s:%d: %w", path, line, err)
		}
	}
}
