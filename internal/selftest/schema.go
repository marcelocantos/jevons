// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package selftest is the 🎯T110 pack orchestrator: classed packs on
// sites live | drill | ci with one report schema. Grades come from
// measurements only; narrative is optional commentary.
package selftest

import "time"

// SchemaVersion is the report schema version (bump on breaking changes).
const SchemaVersion = 1

// Site is where a pack runs.
type Site string

const (
	SiteLive  Site = "live"  // soft L0–L2 safe packs on daily daemon
	SiteDrill Site = "drill" // non-prod isolate for aggressive packs
	SiteCI    Site = "ci"    // unattended hermetic/headless
)

// Grade is derived from measurements (never free-form narrative).
type Grade string

const (
	GradePass  Grade = "pass"
	GradeFail  Grade = "fail"
	GradeSkip  Grade = "skip"
	GradeError Grade = "error"
)

// Measurement is one graded observation.
type Measurement struct {
	ID        string `json:"id"`
	Value     any    `json:"value"`
	Unit      string `json:"unit,omitempty"`
	Threshold any    `json:"threshold,omitempty"`
	OK        bool   `json:"ok"`
}

// Report is the shared T110 report schema.
type Report struct {
	SchemaVersion int           `json:"schema_version"`
	Site          Site          `json:"site"`
	Pack          string        `json:"pack"`
	StartedAt     time.Time     `json:"started_at"`
	FinishedAt    time.Time     `json:"finished_at"`
	Grade         Grade         `json:"grade"`
	Measurements  []Measurement `json:"measurements"`
	Narrative     string        `json:"narrative,omitempty"`
}

// ValidSite reports whether s is a known site class.
func ValidSite(s Site) bool {
	switch s {
	case SiteLive, SiteDrill, SiteCI:
		return true
	default:
		return false
	}
}

// GradeFromMeasurements derives grade: all ok → pass; any !ok → fail;
// empty measurements → error (caller should have set skip/error already).
func GradeFromMeasurements(ms []Measurement) Grade {
	if len(ms) == 0 {
		return GradeError
	}
	for _, m := range ms {
		if !m.OK {
			return GradeFail
		}
	}
	return GradePass
}
