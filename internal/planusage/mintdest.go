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

// PickMintDest chooses where an omit-provider mint lands (🎯T495).
//
// The candidate set is the mint-eligible greens (DestEligible: ok, under,
// locked — never ahead/hot/exhausted/unpublished). Among them the highest
// weekly remaining % wins. A green that publishes no remaining figure is
// unknown, never 0%: it cannot be beaten on capacity, so it joins the
// equally-obvious set rather than losing automatically. configPref decides
// only when two or more greens are equally obvious (remaining within
// th.MintIndifferencePercent of the best known, or unknown); an ineligible
// or clearly-behind config default never wins while another green exists.
func PickMintDest(cands []DestCand, configPref string, now time.Time, th Thresholds) MintDestPick {
	type green struct {
		prov      string
		remaining *float64
		load      int
	}
	var greens []green
	for _, c := range cands {
		if !DestEligible(c.Backend, now, th) {
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
		if w, ok := c.Backend.Window(WindowWeekly); ok && w.RemainingPercent != nil {
			r := *w.RemainingPercent
			rem = &r
		}
		greens = append(greens, green{prov: p, remaining: rem, load: c.Load})
	}
	if len(greens) == 0 {
		return MintDestPick{}
	}
	// Outright order: most remaining first; known capacity outranks
	// unknown; then least load, then name for determinism.
	sort.SliceStable(greens, func(i, j int) bool {
		gi, gj := greens[i], greens[j]
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
	for _, g := range greens {
		if g.remaining != nil {
			bestKnown = g.remaining
			break
		}
	}
	tie := map[string]bool{}
	for _, g := range greens {
		if g.remaining == nil || bestKnown == nil || *g.remaining >= *bestKnown-th.MintIndifferencePercent {
			tie[g.prov] = true
		}
	}
	cfg := strings.ToLower(strings.TrimSpace(configPref))
	if len(tie) >= 2 && cfg != "" && tie[cfg] {
		return MintDestPick{Provider: cfg, OK: true, ConfigTie: true}
	}
	return MintDestPick{Provider: greens[0].prov, OK: true}
}
