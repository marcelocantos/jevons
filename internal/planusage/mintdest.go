// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"sort"
	"strings"
	"time"
)

// MintDestPick is the usage-first omit-provider mint decision (🎯T495).
type MintDestPick struct {
	// Provider is the chosen green backend, empty when none is eligible.
	Provider string
	// OK is false when no published backend is mint-eligible — the caller
	// must refuse the mint rather than fall back to an ineligible config
	// default.
	OK bool
	// ConfigTie is true when two or more greens were equally obvious and
	// the config preference broke the tie.
	ConfigTie bool
}

// mintBandRank is destination order (🎯T693): locked, then under, then ok.
func mintBandRank(b WeeklyBand) (int, bool) {
	switch b {
	case BandLocked:
		return 0, true
	case BandUnder:
		return 1, true
	case BandOK:
		return 2, true
	default:
		return 0, false
	}
}

// PickMintDest chooses where an omit-provider mint lands (🎯T495 / 🎯T693).
//
// DestEligible greens only (locked, under, ok — never ahead/hot). The
// published band is the primary key. Within a band, remaining % and
// configPref keep the T495 tie rules.
func PickMintDest(cands []DestCand, configPref string, now time.Time, th Thresholds) MintDestPick {
	type green struct {
		prov      string
		remaining *float64
		load      int
		rank      int
	}
	var greens []green
	bestRank := 99
	for _, c := range cands {
		if !DestEligible(c.Backend, now, th) {
			continue
		}
		rank, ok := mintBandRank(WeeklyBandOf(c.Backend, now, th))
		if !ok {
			continue
		}
		p := strings.ToLower(strings.TrimSpace(c.Provider))
		if p == "" {
			p = strings.ToLower(strings.TrimSpace(c.Backend.Provider))
		}
		if p == "" {
			continue
		}
		var rem *float64
		if w, ok := c.Backend.PrimaryAllowanceWindow(); ok && w.RemainingPercent != nil {
			r := *w.RemainingPercent
			rem = &r
		}
		greens = append(greens, green{prov: p, remaining: rem, load: c.Load, rank: rank})
		if rank < bestRank {
			bestRank = rank
		}
	}
	var top []green
	for _, g := range greens {
		if g.rank == bestRank {
			top = append(top, g)
		}
	}
	if len(top) == 0 {
		return MintDestPick{}
	}
	sort.SliceStable(top, func(i, j int) bool {
		gi, gj := top[i], top[j]
		switch {
		case gi.remaining != nil && gj.remaining != nil && *gi.remaining != *gj.remaining:
			return *gi.remaining > *gj.remaining
		case (gi.remaining != nil) != (gj.remaining != nil):
			return gi.remaining != nil
		case gi.load != gj.load:
			return gi.load < gj.load
		default:
			return gi.prov < gj.prov
		}
	})
	var bestKnown *float64
	for _, g := range top {
		if g.remaining != nil {
			bestKnown = g.remaining
			break
		}
	}
	tie := map[string]bool{}
	for _, g := range top {
		if g.remaining == nil || bestKnown == nil || *g.remaining >= *bestKnown-th.MintIndifferencePercent {
			tie[g.prov] = true
		}
	}
	cfg := strings.ToLower(strings.TrimSpace(configPref))
	if len(tie) >= 2 && cfg != "" && tie[cfg] {
		return MintDestPick{Provider: cfg, OK: true, ConfigTie: true}
	}
	return MintDestPick{Provider: top[0].prov, OK: true}
}
