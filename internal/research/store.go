// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package research

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	// DirName is the research subtree under state_dir.
	DirName = "research"
	// NotesFileName is the canonical durable ledger.
	NotesFileName = "notes.json"
	// NotesRenderDir holds the markdown render of each note (owner/PO readable).
	NotesRenderDir = "notes"
)

// Store is the durable research-note ledger: JSON canon plus a markdown render
// per note, both written atomically (write-and-rename).
type Store struct {
	dir string
	mu  sync.Mutex
}

type notesFile struct {
	Version int    `json:"version"`
	Notes   []Note `json:"notes"`
}

// Open loads (or creates) the note store under stateDir/research.
func Open(stateDir string) (*Store, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, fmt.Errorf("research store: stateDir required")
	}
	return OpenDir(filepath.Join(expandHome(stateDir), DirName))
}

// OpenDir opens a store rooted at an explicit directory (tests).
func OpenDir(dir string) (*Store, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("research store: dir required")
	}
	if err := os.MkdirAll(filepath.Join(dir, NotesRenderDir), 0o755); err != nil {
		return nil, fmt.Errorf("research store: mkdir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Dir returns the store root.
func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

// Path returns the canonical ledger path.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return filepath.Join(s.dir, NotesFileName)
}

// NotePath returns the markdown render path for a note id.
func (s *Store) NotePath(id string) string {
	if s == nil {
		return ""
	}
	return filepath.Join(s.dir, NotesRenderDir, id+".md")
}

// List returns all notes, most recently updated first.
func (s *Store) List() ([]Note, error) {
	if s == nil {
		return nil, fmt.Errorf("research store: nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	notes, err := s.load()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].UpdatedAt > notes[j].UpdatedAt })
	return notes, nil
}

// Get returns one note by id or topic.
func (s *Store) Get(idOrTopic string) (Note, error) {
	if s == nil {
		return Note{}, fmt.Errorf("research store: nil")
	}
	want := strings.TrimSpace(idOrTopic)
	if want == "" {
		return Note{}, fmt.Errorf("research store: id required")
	}
	notes, err := s.List()
	if err != nil {
		return Note{}, err
	}
	slug := Slug(want)
	for _, n := range notes {
		if n.ID == want || n.ID == slug || n.Topic == want {
			return n, nil
		}
	}
	return Note{}, fmt.Errorf("research store: note not found: %s", idOrTopic)
}

// Apply folds one cycle's findings into the topic's note and persists both the
// canonical ledger and the markdown render. The note is written even when
// nothing changed (the checked-at stamp is provenance), but no revision is
// appended — see Apply.
func (s *Store) Apply(in RevisionInput) (Note, Delta, error) {
	if s == nil {
		return Note{}, Delta{}, fmt.Errorf("research store: nil")
	}
	topic := strings.TrimSpace(in.Topic)
	if topic == "" {
		return Note{}, Delta{}, fmt.Errorf("research store: topic required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	notes, err := s.load()
	if err != nil {
		return Note{}, Delta{}, err
	}
	id := Slug(topic)
	idx := -1
	for i, n := range notes {
		if n.ID == id {
			idx = i
			break
		}
	}
	base := NewNote(topic, in.Title, in.At)
	if idx >= 0 {
		base = notes[idx]
	}
	updated, delta, err := Apply(base, in)
	if err != nil {
		return Note{}, Delta{}, err
	}
	if idx >= 0 {
		notes[idx] = updated
	} else {
		notes = append(notes, updated)
	}
	if err := s.save(notes); err != nil {
		return Note{}, Delta{}, err
	}
	if err := writeFileAtomic(s.NotePath(updated.ID), []byte(RenderNote(updated)), 0o644); err != nil {
		return Note{}, Delta{}, fmt.Errorf("research store: render %s: %w", updated.ID, err)
	}
	return updated, delta, nil
}

func (s *Store) load() ([]Note, error) {
	data, err := os.ReadFile(s.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("research store: read %s: %w", s.Path(), err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var nf notesFile
	if err := json.Unmarshal(data, &nf); err != nil {
		// Malformed durable state is a hard error, never a silent reset.
		return nil, fmt.Errorf("research store: parse %s: %w", s.Path(), err)
	}
	return nf.Notes, nil
}

func (s *Store) save(notes []Note) error {
	nf := notesFile{Version: 1, Notes: notes}
	if nf.Notes == nil {
		nf.Notes = []Note{}
	}
	data, err := json.MarshalIndent(nf, "", "  ")
	if err != nil {
		return fmt.Errorf("research store: marshal: %w", err)
	}
	return writeFileAtomic(s.Path(), append(data, '\n'), 0o644)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
