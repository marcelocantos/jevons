package treeguard

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store holds, per session, the last content each session observed for each
// guarded path. Observations live under one directory per session so that N
// concurrent workers never write the same file — the guard must not reproduce
// the shared-mutable-state bug it exists to catch (🎯T376).
type Store struct {
	Root string
}

// StoreDirEnv overrides the observation directory (tests, isolates).
const StoreDirEnv = "JEVONS_TREEGUARD_DIR"

// DefaultStoreRoot is ~/.jevons/treeguard unless StoreDirEnv is set.
func DefaultStoreRoot() string {
	if dir := os.Getenv(StoreDirEnv); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "jevons-treeguard")
	}
	return filepath.Join(home, ".jevons", "treeguard")
}

// Record saves the content a session has just seen on disk for absPath.
func (s *Store) Record(session, absPath string, content []byte, now time.Time) error {
	dir := filepath.Join(s.Root, sessionDir(session))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	base := filepath.Join(dir, pathKey(absPath))
	if err := writeAtomic(base+".blob", content); err != nil {
		return err
	}
	meta, err := json.Marshal(Observation{
		Path: absPath,
		Hash: HashBytes(content),
		At:   now,
	})
	if err != nil {
		return err
	}
	return writeAtomic(base+".json", meta)
}

// Lookup returns what the session last observed for absPath. A session that
// never looked yields (nil, nil, nil) — absence is not an error, it is the
// "never-observed" case the policy denies on.
func (s *Store) Lookup(session, absPath string) (*Observation, []byte, error) {
	base := filepath.Join(s.Root, sessionDir(session), pathKey(absPath))
	meta, err := os.ReadFile(base + ".json")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var obs Observation
	if err := json.Unmarshal(meta, &obs); err != nil {
		// A torn observation must not be silently treated as "no observation":
		// that would downgrade a stale-base denial into a never-observed one
		// and still block, but with a misleading reason.
		return nil, nil, err
	}
	content, err := os.ReadFile(base + ".blob")
	if errors.Is(err, os.ErrNotExist) {
		return &obs, nil, nil
	}
	if err != nil {
		return &obs, nil, err
	}
	return &obs, content, nil
}

// Prune drops observation directories whose newest entry is older than maxAge,
// keeping ~/.jevons/treeguard from growing without bound across sessions.
func (s *Store) Prune(maxAge time.Duration, now time.Time) error {
	entries, err := os.ReadDir(s.Root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(s.Root, e.Name())
		info, err := os.Stat(dir)
		if err != nil || now.Sub(info.ModTime()) <= maxAge {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	return nil
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".treeguard-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

const pathKeyLen = 24

func pathKey(absPath string) string {
	return HashBytes([]byte(absPath))[:pathKeyLen]
}

func sessionDir(session string) string {
	if session == "" {
		return "unknown-session"
	}
	var b strings.Builder
	for _, r := range session {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
