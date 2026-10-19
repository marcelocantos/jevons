// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package repowatch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RepoState is the durable per-repo watermark under state_dir/repowatch/<slug>.json.
type RepoState struct {
	Repo      string            `json:"repo"`
	PO        string            `json:"po,omitempty"`
	Workdir   string            `json:"workdir,omitempty"`
	// LastSeen maps item Key() → updated_at RFC3339 of the last delivered view.
	LastSeen  map[string]string `json:"last_seen,omitempty"`
	LastPoll  string            `json:"last_poll,omitempty"`
	LastError string            `json:"last_error,omitempty"`
	Polls     int               `json:"polls,omitempty"`
	Delivered int               `json:"delivered,omitempty"`
}

// repoSlug turns "owner/name" into a filesystem-safe name.
func repoSlug(repo string) string {
	s := strings.TrimSpace(repo)
	s = strings.ReplaceAll(s, "/", "__")
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, s)
	if s == "" {
		return "_unknown"
	}
	return s
}

// RepoStatePath is the durable path for one repo's watermark.
func RepoStatePath(stateDir, repo string) string {
	return filepath.Join(expandHome(stateDir), DirName, repoSlug(repo)+".json")
}

// LoadRepoState reads a watermark (absent = empty).
func LoadRepoState(stateDir, repo string) (RepoState, error) {
	path := RepoStatePath(stateDir, repo)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return RepoState{Repo: repo, LastSeen: map[string]string{}}, nil
		}
		return RepoState{}, fmt.Errorf("repowatch state: read %s: %w", path, err)
	}
	var st RepoState
	if err := json.Unmarshal(data, &st); err != nil {
		return RepoState{}, fmt.Errorf("repowatch state: parse %s: %w", path, err)
	}
	if st.LastSeen == nil {
		st.LastSeen = map[string]string{}
	}
	if st.Repo == "" {
		st.Repo = repo
	}
	return st, nil
}

// SaveRepoState persists a watermark atomically.
func SaveRepoState(stateDir string, st RepoState) error {
	if strings.TrimSpace(st.Repo) == "" {
		return fmt.Errorf("repowatch state: repo required")
	}
	if st.LastSeen == nil {
		st.LastSeen = map[string]string{}
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("repowatch state: marshal: %w", err)
	}
	return writeFileAtomic(RepoStatePath(stateDir, st.Repo), append(data, '\n'), 0o644)
}

// Delta reports items that are new or whose UpdatedAt is strictly after the
// last delivered view. Pure: the caller decides whether to commit the marker.
func Delta(st RepoState, items []Item) (fresh []Item, next RepoState) {
	next = st
	if next.LastSeen == nil {
		next.LastSeen = map[string]string{}
	}
	for _, it := range items {
		key := it.Key()
		ts := ""
		if !it.UpdatedAt.IsZero() {
			ts = it.UpdatedAt.UTC().Format(time.RFC3339Nano)
		}
		prev, seen := next.LastSeen[key]
		if !seen || ts > prev {
			fresh = append(fresh, it)
			if ts != "" {
				next.LastSeen[key] = ts
			} else if !seen {
				next.LastSeen[key] = time.Now().UTC().Format(time.RFC3339Nano)
			}
		}
	}
	return fresh, next
}

// ListRepoStates enumerates every per-repo watermark under the state dir.
func ListRepoStates(stateDir string) ([]RepoState, error) {
	dir := filepath.Join(expandHome(stateDir), DirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []RepoState
	for _, e := range entries {
		if e.IsDir() || e.Name() == ConfigFileName || e.Name() == StateFileName {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var st RepoState
		if err := json.Unmarshal(data, &st); err != nil || st.Repo == "" {
			continue
		}
		out = append(out, st)
	}
	return out, nil
}
