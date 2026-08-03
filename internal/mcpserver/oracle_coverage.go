// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
)

// RegionStatus is the pinned / fuzzy / taste / spike bucket for one
// design aspect on an oracle-coverage map (🎯T31.2).
type RegionStatus int

const (
	// RegionFuzzy: intent still open — production work refused until pinned.
	RegionFuzzy RegionStatus = iota
	// RegionPinned: executable check(s) seed from load-bearing examples.
	RegionPinned
	// RegionTaste: irreducible class-3 perceptual residue (single owner gate).
	RegionTaste
	// RegionSpike: exploratory on purpose; not production, intentionally un-oracled.
	RegionSpike
)

func (s RegionStatus) String() string {
	switch s {
	case RegionFuzzy:
		return "fuzzy"
	case RegionPinned:
		return "pinned"
	case RegionTaste:
		return "taste"
	case RegionSpike:
		return "spike"
	default:
		return "unknown"
	}
}

// LoadBearingExample is the unit of greenfield intent transfer: when X, expect Y.
type LoadBearingExample struct {
	When   string `json:"when"`
	Expect string `json:"expect"`
	Source string `json:"source,omitempty"` // e.g. "owner"
}

// CoverageRegion is one aspect of the design on the live oracle-coverage map.
type CoverageRegion struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Status     RegionStatus         `json:"status"`
	Examples   []LoadBearingExample `json:"examples,omitempty"`
	OracleHint string               `json:"oracle_hint,omitempty"` // e.g. go test -run …
	Notes      string               `json:"notes,omitempty"`
}

// CoverageMap is the live map co-developed alongside design (🎯T31.2).
// Pure in-memory model — no I/O; persistence/UI are later surfaces (T29 residual).
type CoverageMap struct {
	Title   string           `json:"title"`
	Regions []CoverageRegion `json:"regions"`
}

// NewCoverageMap returns an empty map for a named design.
func NewCoverageMap(title string) *CoverageMap {
	return &CoverageMap{Title: strings.TrimSpace(title)}
}

// AddFuzzy registers a still-open design region (default status for new intent).
func (m *CoverageMap) AddFuzzy(id, name string) error {
	if m == nil {
		return fmt.Errorf("nil coverage map")
	}
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if id == "" || name == "" {
		return fmt.Errorf("id and name required")
	}
	if m.regionIndex(id) >= 0 {
		return fmt.Errorf("region %q already exists", id)
	}
	m.Regions = append(m.Regions, CoverageRegion{
		ID:     id,
		Name:   name,
		Status: RegionFuzzy,
	})
	return nil
}

// AddExample appends a load-bearing when/expect pair elicited from the owner.
func (m *CoverageMap) AddExample(regionID string, ex LoadBearingExample) error {
	if m == nil {
		return fmt.Errorf("nil coverage map")
	}
	ex.When = strings.TrimSpace(ex.When)
	ex.Expect = strings.TrimSpace(ex.Expect)
	if ex.When == "" || ex.Expect == "" {
		return fmt.Errorf("when and expect required")
	}
	i := m.regionIndex(regionID)
	if i < 0 {
		return fmt.Errorf("unknown region %q", regionID)
	}
	if ex.Source == "" {
		ex.Source = "owner"
	}
	m.Regions[i].Examples = append(m.Regions[i].Examples, ex)
	return nil
}

// Pin marks a region as pinned by an executable oracle. Requires at least one
// load-bearing example (Goodhart guard: examples seed the check, not free prose).
func (m *CoverageMap) Pin(regionID, oracleHint string) error {
	if m == nil {
		return fmt.Errorf("nil coverage map")
	}
	i := m.regionIndex(regionID)
	if i < 0 {
		return fmt.Errorf("unknown region %q", regionID)
	}
	if len(m.Regions[i].Examples) == 0 {
		return fmt.Errorf("region %q has no load-bearing examples; elicit when/expect first", regionID)
	}
	hint := strings.TrimSpace(oracleHint)
	if hint == "" {
		return fmt.Errorf("oracle hint required to pin (named check / test command)")
	}
	m.Regions[i].Status = RegionPinned
	m.Regions[i].OracleHint = hint
	return nil
}

// MarkTaste isolates irreducible class-3 perceptual residue as a single
// owner accept/reject — never mixed into decidable production gates.
func (m *CoverageMap) MarkTaste(regionID string) error {
	return m.setStatus(regionID, RegionTaste)
}

// MarkSpike marks exploratory work intentionally un-oracled (proportionality).
// Spikes are not production; ProductionBlocked does not include them.
func (m *CoverageMap) MarkSpike(regionID string) error {
	return m.setStatus(regionID, RegionSpike)
}

// MarkFuzzy reopens a region (intent still moving).
func (m *CoverageMap) MarkFuzzy(regionID string) error {
	return m.setStatus(regionID, RegionFuzzy)
}

func (m *CoverageMap) setStatus(regionID string, st RegionStatus) error {
	if m == nil {
		return fmt.Errorf("nil coverage map")
	}
	i := m.regionIndex(regionID)
	if i < 0 {
		return fmt.Errorf("unknown region %q", regionID)
	}
	m.Regions[i].Status = st
	return nil
}

// FuzzyIDs returns region IDs still fuzzy (production-blocked).
func (m *CoverageMap) FuzzyIDs() []string {
	if m == nil {
		return nil
	}
	var out []string
	for _, r := range m.Regions {
		if r.Status == RegionFuzzy {
			out = append(out, r.ID)
		}
	}
	return out
}

// ProductionBlocked is true when any region is still fuzzy.
// SPIRAL rule: refuse production work on still-fuzzy parts.
func (m *CoverageMap) ProductionBlocked() bool {
	return len(m.FuzzyIDs()) > 0
}

// AllowsProduction is true when the named region is pinned or taste
// (taste is owner-gated, not agent production) or the map has no fuzzy
// blockers for that id. Fuzzy regions refuse production; spikes refuse
// production claims (exploratory only).
func (m *CoverageMap) AllowsProduction(regionID string) bool {
	if m == nil {
		return false
	}
	i := m.regionIndex(regionID)
	if i < 0 {
		return false
	}
	switch m.Regions[i].Status {
	case RegionPinned:
		return true
	case RegionTaste:
		// Taste residue is owner accept/reject, not agent production work.
		return false
	case RegionSpike:
		return false
	default: // fuzzy
		return false
	}
}

// SummaryMarkdown renders the live map for design dialogue (text path;
// rich T29 surface is residual).
func (m *CoverageMap) SummaryMarkdown() string {
	if m == nil {
		return ""
	}
	var b strings.Builder
	title := m.Title
	if title == "" {
		title = "oracle-coverage map"
	}
	b.WriteString("# Oracle-coverage map: ")
	b.WriteString(title)
	b.WriteByte('\n')
	if len(m.Regions) == 0 {
		b.WriteString("\n_(empty — add fuzzy regions from owner intent)_\n")
		return b.String()
	}
	for _, r := range m.Regions {
		fmt.Fprintf(&b, "\n## [%s] %s — %s\n", r.Status.String(), r.ID, r.Name)
		if r.OracleHint != "" {
			fmt.Fprintf(&b, "- oracle: `%s`\n", r.OracleHint)
		}
		if r.Notes != "" {
			fmt.Fprintf(&b, "- notes: %s\n", r.Notes)
		}
		for _, ex := range r.Examples {
			fmt.Fprintf(&b, "- when **%s** expect **%s**", ex.When, ex.Expect)
			if ex.Source != "" {
				fmt.Fprintf(&b, " _(source: %s)_", ex.Source)
			}
			b.WriteByte('\n')
		}
		if len(r.Examples) == 0 && r.Status == RegionFuzzy {
			b.WriteString("- _(no load-bearing examples yet)_\n")
		}
	}
	if m.ProductionBlocked() {
		b.WriteString("\n**Production blocked** on fuzzy: ")
		b.WriteString(strings.Join(m.FuzzyIDs(), ", "))
		b.WriteByte('\n')
	}
	return b.String()
}

func (m *CoverageMap) regionIndex(id string) int {
	id = strings.TrimSpace(id)
	for i := range m.Regions {
		if m.Regions[i].ID == id {
			return i
		}
	}
	return -1
}

// --- Spiral process (design → slice → react → sharpen → oracle) ---

// SpiralPhase is one step of the greenfield design spiral (🎯T31.2).
type SpiralPhase int

const (
	SpiralDesign SpiralPhase = iota
	SpiralThinSlice
	SpiralOwnerReact
	SpiralIntentSharpen
	SpiralNewOracle
)

func (p SpiralPhase) String() string {
	switch p {
	case SpiralDesign:
		return "design"
	case SpiralThinSlice:
		return "thin_slice"
	case SpiralOwnerReact:
		return "owner_react"
	case SpiralIntentSharpen:
		return "intent_sharpen"
	case SpiralNewOracle:
		return "new_oracle"
	default:
		return "unknown"
	}
}

// SpiralNext advances the spiral (cycles after new_oracle → design).
func SpiralNext(p SpiralPhase) SpiralPhase {
	switch p {
	case SpiralDesign:
		return SpiralThinSlice
	case SpiralThinSlice:
		return SpiralOwnerReact
	case SpiralOwnerReact:
		return SpiralIntentSharpen
	case SpiralIntentSharpen:
		return SpiralNewOracle
	case SpiralNewOracle:
		return SpiralDesign
	default:
		return SpiralDesign
	}
}

// SpiralAllowsProductionClaims is true only after new_oracle phase when
// the map has no fuzzy regions. Thin slices / owner react may explore;
// production retire claims need pinned intent.
func SpiralAllowsProductionClaims(phase SpiralPhase, m *CoverageMap) bool {
	if m == nil || m.ProductionBlocked() {
		return false
	}
	// Production claims are refused while still in early discovery phases
	// if the map is empty (nothing pinned yet).
	if len(m.Regions) == 0 {
		return false
	}
	// Any phase may claim production only when nothing is fuzzy and at
	// least one region is pinned (not only taste/spike).
	hasPinned := false
	for _, r := range m.Regions {
		if r.Status == RegionPinned {
			hasPinned = true
			break
		}
	}
	return hasPinned
}

// --- Decidable-from-taste sort (rule 1 upstream) ---

// IntentClass separates decidable criteria from irreducible taste.
type IntentClass int

const (
	// IntentDecidable: functional when/expect or named check language.
	IntentDecidable IntentClass = iota
	// IntentTaste: feel / polish / aesthetic without a concrete example.
	IntentTaste
	// IntentAmbiguous: needs elicitation (neither clear example nor pure taste).
	IntentAmbiguous
)

func (c IntentClass) String() string {
	switch c {
	case IntentDecidable:
		return "decidable"
	case IntentTaste:
		return "taste"
	case IntentAmbiguous:
		return "ambiguous"
	default:
		return "unknown"
	}
}

var tasteMarkers = []string{
	"feels",
	"feel right",
	"feel nice",
	"feel good",
	"should feel",
	"looks nice",
	"looks good",
	"pretty",
	"beautiful",
	"polish",
	"aesthetic",
	"taste",
	"vibes",
	"nice ux",
	"delightful",
	"i'll know it when i see it",
	"know it when i see",
}

var decidableMarkers = []string{
	"must ",
	"should return",
	"invariant",
	"property:",
	"test ",
	"assert",
	"exit code",
	"http ",
	"status ",
	"error if",
	"fails when",
	"succeeds when",
}

// ClassifyDesignClause is a hermetic heuristic: sort intent into
// decidable / taste / ambiguous before production work (🎯T31.2).
// Mixed taste + decidable language forces ambiguous so clauses get split
// (target must not inherit taste p into a decidable criterion).
func ClassifyDesignClause(text string) IntentClass {
	s := strings.ToLower(strings.TrimSpace(text))
	if s == "" {
		return IntentAmbiguous
	}
	_, hasExample := ParseLoadBearingExample(text)
	hasTaste := false
	for _, m := range tasteMarkers {
		if strings.Contains(s, m) {
			hasTaste = true
			break
		}
	}
	hasDec := hasExample
	if !hasDec {
		for _, m := range decidableMarkers {
			if strings.Contains(s, m) {
				hasDec = true
				break
			}
		}
	}
	switch {
	case hasDec && hasTaste:
		return IntentAmbiguous
	case hasDec:
		return IntentDecidable
	case hasTaste:
		return IntentTaste
	default:
		return IntentAmbiguous
	}
}

// ParseLoadBearingExample extracts "when X, expect Y" / "when X expect Y"
// forms from owner dialogue. Returns false if no clear pair.
func ParseLoadBearingExample(text string) (LoadBearingExample, bool) {
	s := strings.TrimSpace(text)
	if s == "" {
		return LoadBearingExample{}, false
	}
	lower := strings.ToLower(s)
	// Prefer ", expect" then " expect " after a when.
	wi := strings.Index(lower, "when ")
	if wi < 0 {
		return LoadBearingExample{}, false
	}
	rest := s[wi+len("when "):]
	restLower := strings.ToLower(rest)
	// Split on ", expect" or " expect "
	var whenPart, expectPart string
	if ei := strings.Index(restLower, ", expect "); ei >= 0 {
		whenPart = strings.TrimSpace(rest[:ei])
		expectPart = strings.TrimSpace(rest[ei+len(", expect "):])
	} else if ei := strings.Index(restLower, " expect "); ei >= 0 {
		whenPart = strings.TrimSpace(rest[:ei])
		expectPart = strings.TrimSpace(rest[ei+len(" expect "):])
	} else {
		return LoadBearingExample{}, false
	}
	// Trim trailing punctuation on expect.
	expectPart = strings.TrimRight(expectPart, ".!;")
	whenPart = strings.TrimRight(whenPart, ",;:")
	if whenPart == "" || expectPart == "" {
		return LoadBearingExample{}, false
	}
	return LoadBearingExample{
		When:   whenPart,
		Expect: expectPart,
		Source: "owner",
	}, true
}
