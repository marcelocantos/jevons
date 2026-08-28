// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/marcelocantos/jevons/internal/config"
)

// 🎯T200: domain portfolios — declarative multi-repo grouping without a GM.
//
// Config declares named portfolios and membership paths. The HTTP feed
// matches those paths against live fleet agent workdirs (containment after
// normalization). No agent-name parsing. Empty/missing portfolios return
// a calm empty list.

// PortfolioMemberView is one product/repo slot inside a portfolio, with
// any currently registered agents whose workdir matches the member path.
type PortfolioMemberView struct {
	Path   string   `json:"path"`
	Label  string   `json:"label"`
	Agents []string `json:"agents"`
}

// PortfolioView is one named domain group for the owner-visible surface.
type PortfolioView struct {
	ID      string                `json:"id"`
	Name    string                `json:"name"`
	Members []PortfolioMemberView `json:"members"`
}

// PortfoliosResponse is GET /api/portfolios JSON.
type PortfoliosResponse struct {
	Portfolios []PortfolioView `json:"portfolios"`
}

// normalizePortfolioPath collapses separators and home spellings for matching.
func normalizePortfolioPath(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\\", "/")
	// Collapse repeated slashes without resolving ".." (member paths are fragments).
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	s = strings.TrimRight(s, "/")
	// Map absolute home prefixes to ~ for stable substring match either way.
	if strings.HasPrefix(s, "/Users/") {
		if i := strings.Index(s[len("/Users/"):], "/"); i >= 0 {
			// /Users/<name>/rest → ~/rest
			rest := s[len("/Users/")+i:]
			s = "~" + rest
		}
	}
	return s
}

// workdirMatchesMember reports whether agent workdir contains the member path.
// Membership is declarative path containment — never agent name heuristics.
func workdirMatchesMember(workdir, memberPath string) bool {
	wd := normalizePortfolioPath(workdir)
	mp := normalizePortfolioPath(memberPath)
	if wd == "" || mp == "" {
		return false
	}
	if wd == mp {
		return true
	}
	// Containment on either spelling (~ vs absolute already normalized to ~).
	if strings.Contains(wd, mp) {
		return true
	}
	// Also try bare github.com/org/repo against full workdir without ~ rewrite
	// when member is an absolute path and workdir is relative fragment.
	rawWD := strings.ReplaceAll(strings.TrimRight(strings.ReplaceAll(workdir, "\\", "/"), "/"), "//", "/")
	rawMP := strings.ReplaceAll(strings.TrimRight(strings.ReplaceAll(memberPath, "\\", "/"), "/"), "//", "/")
	if rawWD != "" && rawMP != "" && (rawWD == rawMP || strings.Contains(rawWD, rawMP)) {
		return true
	}
	return false
}

// memberLabel is the short product name for chrome (last path segment).
func memberLabel(memberPath string) string {
	p := strings.Trim(strings.ReplaceAll(memberPath, "\\", "/"), "/")
	if p == "" {
		return ""
	}
	base := path.Base(p)
	if base == "." || base == "/" {
		return p
	}
	return base
}

// BuildPortfolioViews joins declarative portfolio config with live agents.
// agents may be nil/empty. Missing portfolios → empty non-nil slice (calm).
func BuildPortfolioViews(defs []config.Portfolio, agents []agentInfo) []PortfolioView {
	if len(defs) == 0 {
		return []PortfolioView{}
	}
	out := make([]PortfolioView, 0, len(defs))
	for _, d := range defs {
		id := strings.TrimSpace(d.ID)
		if id == "" {
			continue
		}
		name := d.DisplayName()
		members := make([]PortfolioMemberView, 0, len(d.Members))
		for _, m := range d.Members {
			mp := strings.TrimSpace(m)
			if mp == "" {
				continue
			}
			var names []string
			for _, a := range agents {
				if workdirMatchesMember(a.WorkDir, mp) {
					n := strings.TrimSpace(a.Name)
					if n != "" {
						names = append(names, n)
					}
				}
			}
			sort.Strings(names)
			if names == nil {
				names = []string{}
			}
			members = append(members, PortfolioMemberView{
				Path:   mp,
				Label:  memberLabel(mp),
				Agents: names,
			})
		}
		if members == nil {
			members = []PortfolioMemberView{}
		}
		out = append(out, PortfolioView{
			ID:      id,
			Name:    name,
			Members: members,
		})
	}
	return out
}

// SetPortfolios installs the declarative portfolio registry (🎯T200).
// Nil/empty is calm — GET /api/portfolios returns {"portfolios":[]}.
func (s *Server) SetPortfolios(list []config.Portfolio) {
	if s == nil {
		return
	}
	// Defensive copy so callers can reuse the slice.
	cp := make([]config.Portfolio, len(list))
	copy(cp, list)
	s.mu.Lock()
	s.portfolios = cp
	s.mu.Unlock()
}

// handleListPortfolios returns declared portfolios with workdir-matched agents.
func (s *Server) handleListPortfolios(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defs := append([]config.Portfolio(nil), s.portfolios...)
	reg := s.registry
	progress := s.agentProgress
	models := s.fleetModels
	s.mu.RUnlock()

	var agents []agentInfo
	if reg != nil {
		agents = listFleetAgentsNotifying(reg, s.RemovalAccount(), nil, progress, models)
	} else {
		agents = []agentInfo{}
	}
	views := BuildPortfolioViews(defs, agents)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(PortfoliosResponse{Portfolios: views})
}
