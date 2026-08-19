package attrib

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/treeguard"
)

// Backfill exists for exactly one situation: a pile that predates the feed.
// The live hook records touches as they happen; the 2026-08-15 pile happened
// before there was anything to record them, so its evidence has to be
// recovered from what the stopped workers left elsewhere — treeguard's
// observation store and their session transcripts. Each backfilled record
// carries its Via class so the reader can weigh a reconstruction differently
// from an observation.
//
// A backfill is idempotent per (session, source): re-running it must not
// double every record, so each session that has been drained from a source
// carries a marker file and is skipped thereafter.

func backfillMarker(root, session, source string) string {
	return filepath.Join(root, "sessions", sanitize(session), "backfill-"+source+".done")
}

func backfillDone(root, session, source string) bool {
	_, err := os.Stat(backfillMarker(root, session, source))
	return err == nil
}

func markBackfillDone(root, session, source string) error {
	p := backfillMarker(root, session, source)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
}

// BackfillTreeguard converts treeguard's per-session observations into touch
// records. Direct, non-transcript evidence — but treeguard only ever observed
// the guarded hot files, so this fills in a handful of paths, not the pile.
func BackfillTreeguard(store *Store, tgRoot, repoRoot string) (int, error) {
	sessionsDir := tgRoot
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	total := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		session := e.Name()
		if backfillDone(store.Root, session, "treeguard") {
			continue
		}
		metas, err := filepath.Glob(filepath.Join(sessionsDir, session, "*.json"))
		if err != nil {
			continue
		}
		var records []Record
		for _, m := range metas {
			data, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			var obs treeguard.Observation
			if err := json.Unmarshal(data, &obs); err != nil || obs.Path == "" {
				continue
			}
			rel, ok := RelPath(repoRoot, obs.Path)
			if !ok {
				continue
			}
			records = append(records, Record{
				Session: session,
				Path:    rel,
				At:      obs.At.UTC(),
				Via:     ViaTreeguard,
			})
		}
		if err := store.Append(session, records); err != nil {
			return total, err
		}
		if err := markBackfillDone(store.Root, session, "treeguard"); err != nil {
			return total, err
		}
		total += len(records)
	}
	return total, nil
}

// filePathRe matches the file_path (and notebook_path) arguments of mutating
// tool calls as they appear serialized in a session transcript. A regex over
// lines rather than a JSON parse of every transcript schema, deliberately:
// Claude, Grok and Codex transcripts disagree about everything except that a
// tool call's path argument survives serialization as `"file_path":"…"`, and
// a backfill that only understands one provider's schema attributes only one
// provider's workers.
var filePathRe = regexp.MustCompile(`"(?:file_path|notebook_path)"\s*:\s*"((?:[^"\\]|\\.)+)"`)

var timestampRe = regexp.MustCompile(`"timestamp"\s*:\s*"([^"]+)"`)

// BackfillTranscripts scans session transcript files for the paths their
// mutating tool calls named. The session id is the file's base name — true
// for Claude Code and claudia-managed session JSONLs alike — and every record
// is ViaTranscript: complete evidence for a pile that predates the feed, but
// a reconstruction, and marked as one.
//
// Only paths inside repoRoot are kept. The regex sees Read calls' file_path
// too; that overcounts touches toward "looked at it", never misses a write,
// and the Via class already tells the reader this is reconstruction, so the
// asymmetry is the right one: a backfill that missed writes would be worse
// than one that includes reads.
func BackfillTranscripts(store *Store, repoRoot string, files []string) (int, error) {
	total := 0
	for _, f := range files {
		session := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
		if session == "" || backfillDone(store.Root, session, "transcript") {
			continue
		}
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		info, _ := fh.Stat()
		fallbackAt := time.Time{}
		if info != nil {
			fallbackAt = info.ModTime().UTC()
		}
		seen := map[string]bool{}
		var records []Record
		scanner := bufio.NewScanner(fh)
		scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			matches := filePathRe.FindAllStringSubmatch(line, -1)
			if len(matches) == 0 {
				continue
			}
			at := fallbackAt
			if ts := timestampRe.FindStringSubmatch(line); ts != nil {
				if t, err := time.Parse(time.RFC3339, ts[1]); err == nil {
					at = t.UTC()
				}
			}
			for _, m := range matches {
				path := unescapeJSONString(m[1])
				rel, ok := RelPath(repoRoot, path)
				if !ok || seen[rel] {
					continue
				}
				seen[rel] = true
				records = append(records, Record{
					Session: session,
					Path:    rel,
					At:      at,
					Via:     ViaTranscript,
				})
			}
		}
		fh.Close()
		if err := store.Append(session, records); err != nil {
			return total, err
		}
		if err := markBackfillDone(store.Root, session, "transcript"); err != nil {
			return total, err
		}
		total += len(records)
	}
	return total, nil
}

// unescapeJSONString undoes the escapes the regex allowed through. Only the
// forms that appear in real paths; anything stranger stays literal rather
// than failing the record.
func unescapeJSONString(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var out string
	if err := json.Unmarshal([]byte(`"`+s+`"`), &out); err != nil {
		return s
	}
	return out
}
