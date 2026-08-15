package attrib

import (
	"sort"
	"time"
)

// AgentTouch summarises one agent's contact with one path.
type AgentTouch struct {
	Agent string    `json:"agent"`
	First time.Time `json:"first"`
	Last  time.Time `json:"last"`
	Count int       `json:"count"`
	Via   []string  `json:"via"`
}

// PathOwner is one dirty path and everyone who touched it.
type PathOwner struct {
	Path    string       `json:"path"`
	XY      string       `json:"status"`
	Touches []AgentTouch `json:"touches"`
}

// Contested reports whether more than one agent touched the path. A contested
// path is the one case where discarding an agent's slice cannot be a local
// decision — somebody else's edits are in the same file — so it is named rather
// than silently folded into whichever agent touched it last.
func (p PathOwner) Contested() bool { return len(p.Touches) > 1 }

// Owner is the agent with the most recent touch, or UnknownAgent.
func (p PathOwner) Owner() string {
	if len(p.Touches) == 0 {
		return UnknownAgent
	}
	return p.Touches[0].Agent
}

// Slice is one agent's share of the uncommitted pile.
type Slice struct {
	Agent string `json:"agent"`
	// Sole paths only this agent touched: safe to recover or discard alone.
	Sole []PathOwner `json:"sole"`
	// Shared paths this agent touched alongside others: recoverable, but not
	// discardable without deciding what happens to the other agents' edits.
	Shared []PathOwner `json:"shared"`
}

// Paths returns the slice's paths, shared ones included only when asked.
func (s Slice) Paths(includeShared bool) []string {
	var out []string
	for _, p := range s.Sole {
		out = append(out, p.Path)
	}
	if includeShared {
		for _, p := range s.Shared {
			out = append(out, p.Path)
		}
	}
	sort.Strings(out)
	return out
}

// Attribution is the whole answer: every dirty path, sliced per agent, plus
// what nothing accounts for.
type Attribution struct {
	Slices []Slice `json:"slices"`
	// Unattributed dirty paths no record claims. Reported rather than hidden:
	// a tool that quietly omits what it cannot explain teaches its reader that
	// the listed slices are the whole pile, which is how the other half gets
	// deleted by a later bulk checkout.
	Unattributed []PathOwner `json:"unattributed"`
}

// Slice returns one agent's slice.
func (a Attribution) Slice(agent string) (Slice, bool) {
	for _, s := range a.Slices {
		if s.Agent == agent {
			return s, true
		}
	}
	return Slice{}, false
}

// Attribute folds touch records against the current dirty tree.
//
// The intersection with `git status` is what makes this an answer about
// UNFINISHED work: a path an agent touched and then committed is not part of
// anybody's leftovers, and carrying it would bury the paths that are.
func Attribute(records []Record, dirty []DirtyPath) Attribution {
	byPath := map[string]map[string]*AgentTouch{}
	for _, r := range records {
		agent := r.Agent
		if agent == "" {
			agent = UnknownAgent
		}
		agents := byPath[r.Path]
		if agents == nil {
			agents = map[string]*AgentTouch{}
			byPath[r.Path] = agents
		}
		t := agents[agent]
		if t == nil {
			t = &AgentTouch{Agent: agent, First: r.At, Last: r.At}
			agents[agent] = t
		}
		t.Count++
		if r.At.Before(t.First) {
			t.First = r.At
		}
		if r.At.After(t.Last) {
			t.Last = r.At
		}
		if r.Via != "" && !contains(t.Via, r.Via) {
			t.Via = append(t.Via, r.Via)
		}
	}

	var attributed Attribution
	byAgent := map[string]*Slice{}
	for _, d := range dirty {
		owner := PathOwner{Path: d.Path, XY: d.XY}
		for _, t := range byPath[d.Path] {
			owner.Touches = append(owner.Touches, *t)
		}
		sort.Slice(owner.Touches, func(i, j int) bool {
			if owner.Touches[i].Last.Equal(owner.Touches[j].Last) {
				return owner.Touches[i].Agent < owner.Touches[j].Agent
			}
			return owner.Touches[i].Last.After(owner.Touches[j].Last)
		})
		if len(owner.Touches) == 0 {
			attributed.Unattributed = append(attributed.Unattributed, owner)
			continue
		}
		for _, t := range owner.Touches {
			s := byAgent[t.Agent]
			if s == nil {
				s = &Slice{Agent: t.Agent}
				byAgent[t.Agent] = s
			}
			if owner.Contested() {
				s.Shared = append(s.Shared, owner)
			} else {
				s.Sole = append(s.Sole, owner)
			}
		}
	}
	for _, s := range byAgent {
		sort.Slice(s.Sole, func(i, j int) bool { return s.Sole[i].Path < s.Sole[j].Path })
		sort.Slice(s.Shared, func(i, j int) bool { return s.Shared[i].Path < s.Shared[j].Path })
		attributed.Slices = append(attributed.Slices, *s)
	}
	sort.Slice(attributed.Slices, func(i, j int) bool {
		return attributed.Slices[i].Agent < attributed.Slices[j].Agent
	})
	sort.Slice(attributed.Unattributed, func(i, j int) bool {
		return attributed.Unattributed[i].Path < attributed.Unattributed[j].Path
	})
	return attributed
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
