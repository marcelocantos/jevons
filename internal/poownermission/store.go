// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package poownermission

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FileName is the store file under the daemon agent-report state dir.
const FileName = "po-owner-missions.json"

type fileState struct {
	Version int       `json:"version"`
	Open    []Mission `json:"open"`
}

// Store persists open owner missions per PO.
type Store struct {
	mu   sync.Mutex
	path string
}

// OpenStore loads or creates the store at dir/FileName.
func OpenStore(dir string) (*Store, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, errors.New("po owner mission store requires a directory")
	}
	return &Store{path: filepath.Join(dir, FileName)}, nil
}

func (s *Store) load() (fileState, error) {
	if s == nil || s.path == "" {
		return fileState{}, errors.New("unwired store")
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileState{Version: 1}, nil
		}
		return fileState{}, err
	}
	var st fileState
	if err := json.Unmarshal(b, &st); err != nil {
		return fileState{}, err
	}
	if st.Version == 0 {
		st.Version = 1
	}
	return st, nil
}

func (s *Store) save(st fileState) error {
	if st.Version == 0 {
		st.Version = 1
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Open records or replaces the open mission for po (one open mission per PO).
func (s *Store) Open(po string, br BoundedRequest, now time.Time) (Mission, error) {
	po = strings.TrimSpace(po)
	if po == "" {
		return Mission{}, errors.New("po name required")
	}
	if strings.TrimSpace(br.RequestID) == "" {
		return Mission{}, errors.New("request id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return Mission{}, err
	}
	m := Mission{
		RequestID: br.RequestID,
		PO:        po,
		Items:     append([]string(nil), br.Items...),
		ItemCount: br.ItemCount,
		OpenedAt:  now.UTC(),
	}
	var kept []Mission
	for _, x := range st.Open {
		if strings.TrimSpace(x.PO) == po {
			continue
		}
		kept = append(kept, x)
	}
	kept = append(kept, m)
	st.Open = kept
	if err := s.save(st); err != nil {
		return Mission{}, err
	}
	return m, nil
}

// ForPO returns the open mission for po, if any.
func (s *Store) ForPO(po string) (Mission, bool, error) {
	po = strings.TrimSpace(po)
	if po == "" {
		return Mission{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return Mission{}, false, err
	}
	for _, m := range st.Open {
		if strings.TrimSpace(m.PO) == po {
			return m, true, nil
		}
	}
	return Mission{}, false, nil
}

// ListOpen returns every open mission.
func (s *Store) ListOpen() ([]Mission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return nil, err
	}
	return append([]Mission(nil), st.Open...), nil
}

// Close removes the open mission for po.
func (s *Store) Close(po string) error {
	po = strings.TrimSpace(po)
	if po == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return err
	}
	var kept []Mission
	removed := false
	for _, m := range st.Open {
		if strings.TrimSpace(m.PO) == po {
			removed = true
			continue
		}
		kept = append(kept, m)
	}
	if !removed {
		return nil
	}
	st.Open = kept
	return s.save(st)
}

// NoteWake records the last wake time for po's open mission.
func (s *Store) NoteWake(po string, now time.Time) error {
	po = strings.TrimSpace(po)
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return err
	}
	updated := false
	for i := range st.Open {
		if strings.TrimSpace(st.Open[i].PO) != po {
			continue
		}
		st.Open[i].LastWake = now.UTC()
		updated = true
		break
	}
	if !updated {
		return fmt.Errorf("no open owner mission for %q", po)
	}
	return s.save(st)
}
