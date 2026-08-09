// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Closed-aside history (🎯T270): durable ledger of dismissed purpose=aside
// agents so the owner can browse past side-chats and target filings after
// they leave the live RHS 💡 tree. Not session-only memory.
//
// Kind taxonomy (owner-visible delineation):
//   side    — freeform side-chat (aside:)
//   capture — capture: arrest
//   target  — target-filing / target-aside ceremony (target:)
// Unknown/empty kind normalizes to "side".

const (
	asideHistoryFileName = "asides-history.json"
	asideMetaFileName    = "meta.json"
	asideHistoryMaxKeep  = 200 // thin retention residual; oldest drop on append

	asideKindSide    = "side"
	asideKindCapture = "capture"
	asideKindTarget  = "target"
)

// closedAsideRecord is one dismissed aside retained for history browse.
type closedAsideRecord struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Kind        string `json:"kind"` // side | capture | target
	KindLabel   string `json:"kind_label"`
	Parent      string `json:"parent,omitempty"`
	WorkDir     string `json:"workdir,omitempty"`
	ClosedAt    string `json:"closed_at"` // RFC3339 UTC
	ClosedUnix  int64  `json:"closed_unix"`
	Description string `json:"description,omitempty"` // alias of title for clients
}

// asideOpenMeta is written beside the aside workdir while the agent is live
// so dismiss can archive kind without relying on client memory.
type asideOpenMeta struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	Parent    string `json:"parent,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

// normalizeAsideKind maps free-form create/dismiss tags to the closed set.
func normalizeAsideKind(kind string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	switch k {
	case asideKindSide, "aside", "side-chat", "freeform", "":
		return asideKindSide
	case asideKindCapture:
		return asideKindCapture
	case asideKindTarget, "file-target", "target-aside", "filing":
		return asideKindTarget
	default:
		// Preserve unknown tags as side so UI still groups them; callers may
		// pass purpose=file-target which is normalized above.
		if k == "file_target" {
			return asideKindTarget
		}
		return asideKindSide
	}
}

func asideKindLabel(kind string) string {
	switch normalizeAsideKind(kind) {
	case asideKindTarget:
		return "Target filing"
	case asideKindCapture:
		return "Capture"
	default:
		return "Side chat"
	}
}

func (s *Server) asideHistoryPath() string {
	s.mu.RLock()
	dir := s.stateDir
	s.mu.RUnlock()
	return filepath.Join(dir, asideHistoryFileName)
}

func (s *Server) asideMetaPath(id string) string {
	s.mu.RLock()
	dir := s.stateDir
	s.mu.RUnlock()
	return filepath.Join(dir, "asides", strings.TrimSpace(id), asideMetaFileName)
}

// writeAsideOpenMeta persists kind/title for a live aside (create path).
func (s *Server) writeAsideOpenMeta(id, title, kind, parent string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("id is required")
	}
	kind = normalizeAsideKind(kind)
	title = strings.TrimSpace(title)
	if title == "" {
		title = "aside"
	}
	meta := asideOpenMeta{
		ID:        id,
		Title:     title,
		Kind:      kind,
		Parent:    strings.TrimSpace(parent),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	path := s.asideMetaPath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("aside meta dir: %w", err)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write aside meta: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit aside meta: %w", err)
	}
	return nil
}

// readAsideOpenMeta loads create-time meta if present.
func (s *Server) readAsideOpenMeta(id string) (asideOpenMeta, bool) {
	path := s.asideMetaPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		return asideOpenMeta{}, false
	}
	var m asideOpenMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return asideOpenMeta{}, false
	}
	m.Kind = normalizeAsideKind(m.Kind)
	return m, true
}

// archiveClosedAside records a dismissed aside into durable history.
// kindHint may come from DELETE query/body; open meta and defaults fill gaps.
func (s *Server) archiveClosedAside(id, title, kindHint, parent, workdir string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("id is required")
	}
	kind := normalizeAsideKind(kindHint)
	title = strings.TrimSpace(title)
	if meta, ok := s.readAsideOpenMeta(id); ok {
		if kindHint == "" {
			kind = meta.Kind
		}
		if title == "" {
			title = meta.Title
		}
		if parent == "" {
			parent = meta.Parent
		}
	}
	if title == "" {
		title = "aside"
	}
	now := time.Now().UTC()
	rec := closedAsideRecord{
		ID:          id,
		Title:       title,
		Kind:        kind,
		KindLabel:   asideKindLabel(kind),
		Parent:      strings.TrimSpace(parent),
		WorkDir:     strings.TrimSpace(workdir),
		ClosedAt:    now.Format(time.RFC3339),
		ClosedUnix:  now.Unix(),
		Description: title,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Re-read under lock via path (store is file-backed; serialize writers).
	// Nested lock: archive holds s.mu; load/save use paths only — call unlocked helpers.
	list, err := s.loadClosedAsidesUnlocked()
	if err != nil {
		return err
	}
	// Upsert by id (re-dismiss moves to front with new closed time).
	out := make([]closedAsideRecord, 0, len(list)+1)
	out = append(out, rec)
	for _, e := range list {
		if e.ID == id {
			continue
		}
		out = append(out, e)
	}
	if err := s.saveClosedAsidesUnlocked(out); err != nil {
		return err
	}
	return nil
}

// Unlocked load/save for use when s.mu is already held (or pure tests).
func (s *Server) loadClosedAsidesUnlocked() ([]closedAsideRecord, error) {
	path := filepath.Join(s.stateDir, asideHistoryFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var list []closedAsideRecord
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	for i := range list {
		list[i].Kind = normalizeAsideKind(list[i].Kind)
		list[i].KindLabel = asideKindLabel(list[i].Kind)
		if list[i].Description == "" {
			list[i].Description = list[i].Title
		}
	}
	return list, nil
}

func (s *Server) saveClosedAsidesUnlocked(list []closedAsideRecord) error {
	path := filepath.Join(s.stateDir, asideHistoryFileName)
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].ClosedUnix != list[j].ClosedUnix {
			return list[i].ClosedUnix > list[j].ClosedUnix
		}
		return list[i].ID > list[j].ID
	})
	if len(list) > asideHistoryMaxKeep {
		list = list[:asideHistoryMaxKeep]
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// listClosedAsides returns durable history (newest first).
func (s *Server) listClosedAsides() ([]closedAsideRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadClosedAsidesUnlocked()
}
