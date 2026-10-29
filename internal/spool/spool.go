// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package spool reads the Oh My Pi sidecar's dated session log,
// events-YYYY-MM-DD.log (🎯T866), from wherever the sidecar writes it.
// Older dates first.
package spool

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marcelocantos/claudia/omp"
)

// DirEnv overrides the sidecar's own spool directory (tests and isolates).
const DirEnv = "JEVONS_SPOOL_DIR"

// Record is one sidecar line. Each names its seat.
type Record struct {
	TS        time.Time       `json:"-"`
	RawTS     string          `json:"ts"`
	Seat      string          `json:"seat"`
	SessionID string          `json:"session_id,omitempty"`
	Type      string          `json:"type"`
	Stop      string          `json:"stop,omitempty"`
	Text      string          `json:"text,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Provider  string          `json:"provider,omitempty"`
	Model     string          `json:"model,omitempty"`
	CostUSD   *float64        `json:"costUSD,omitempty"`
	Snapshot  json.RawMessage `json:"snapshot,omitempty"`
	UsageFact json.RawMessage `json:"usage_fact,omitempty"`
}

// Dir is JEVONS_SPOOL_DIR when set, else the directory the sidecar itself
// writes: claudia's omp.SpoolDir (CLAUDIA_OMP_SPOOL_DIR, else claudia's
// state dir). Claudia 0145de0 (2026-10-16) moved the sidecar's default out of
// ~/.jevons/spool, and a reader that kept the old default read a dead
// directory: every sidecar seat's evidence, activity and replay went stale on
// the development daemon while isolates, which set both variables to one
// path, stayed green.
func Dir() string {
	if p := strings.TrimSpace(os.Getenv(DirEnv)); p != "" {
		return p
	}
	return omp.SpoolDir()
}

// SeatHasHistory reports whether any dated file holds a record for seat.
// It does not decode snapshots. GET /api/agents calls this for every
// running seat, and a turn_end line is the whole agent state (🎯T868).
func SeatHasHistory(dir, seat string) bool {
	if dir == "" || seat == "" {
		return false
	}
	hits, _, err := seatIndex(dir)
	return err == nil && hits[seat] != ""
}

// ReadSeat returns records for seat, older dates first.
func ReadSeat(dir, seat string) ([]Record, error) {
	return ReadSeatSince(dir, seat, time.Time{})
}

// ReadSeatSince is [ReadSeat] limited to day files written at or after
// since; a zero since reads them all. A file last modified before since
// holds nothing newer, so a caller that wants a recent window does not pay
// for the whole history (🎯T1067: the spool is gigabytes, and a restart
// that recovered every attaching seat's missed turns decoded all of it once
// per seat, growing the daemon about 1 GB a minute).
//
// A line is decoded only if it names the seat. Every line carries a whole
// event, a turn_end its seat's full agent state, so decoding the shared
// files to keep one seat's records was most of the cost.
func ReadSeatSince(dir, seat string, since time.Time) ([]Record, error) {
	if dir == "" || seat == "" {
		return nil, nil
	}
	names, err := listDayFiles(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	key := seatKey(seat)
	var out []Record
	for _, name := range names {
		path := filepath.Join(dir, name)
		if !since.IsZero() {
			if st, err := os.Stat(path); err == nil && st.ModTime().Before(since) {
				continue
			}
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(countingReader{f})
		sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
		for sc.Scan() {
			raw := sc.Bytes()
			if !bytes.Contains(raw, key) {
				continue
			}
			rec, ok := decodeLine(raw)
			if !ok || rec.Seat != seat {
				continue
			}
			out = append(out, rec)
		}
		_ = f.Close()
		if err := sc.Err(); err != nil {
			return out, err
		}
	}
	return out, nil
}

// seatKey is the bytes a line naming seat carries: its "seat" field as
// encoding/json writes it. ReadSeatSince decodes only lines that contain it.
func seatKey(seat string) []byte {
	encoded, _ := json.Marshal(seat) // a string always marshals
	return append([]byte(`"seat":`), encoded...)
}

// decodeLine is one spool line as a Record. ok is false for a blank line
// or one that does not decode; ReadSeatSince skips both, and the phase
// index applies the same rule (🎯T1084) so the two cannot drift.
func decodeLine(raw []byte) (Record, bool) {
	line := bytes.TrimSpace(raw)
	if len(line) == 0 {
		return Record{}, false
	}
	var rec Record
	if err := json.Unmarshal(line, &rec); err != nil {
		return Record{}, false
	}
	rec.TS = parseRecordTS(rec.RawTS)
	return rec, true
}

// parseRecordTS is a record's "ts" as a time, or zero when it does not parse.
func parseRecordTS(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts
	}
	return time.Time{}
}

// LatestPath is the newest dated log that names seat. Empty when the
// seat has no spool history. Readers use this for mtime, not a vendor
// JSONL (🎯T866.4).
func LatestPath(dir, seat string) string {
	if dir == "" || seat == "" {
		return ""
	}
	hits, _, err := seatIndex(dir)
	if err != nil {
		return ""
	}
	return hits[seat]
}

// LatestSeatTime is the "ts" field of the newest spool line naming seat,
// across all dated files. LatestPath's file is shared by every sidecar
// seat writing that day, so the file's mtime is the day's last write from
// ANY seat, not this one's (🎯T893: all anthropic sidecar seats read back
// the identical age because they share one events-YYYY-MM-DD.log). The
// per-line "ts" field is per-seat; this is the value readers must use for
// recency, not os.Stat on LatestPath's result. The ok=false zero value
// means no readable ts was found for seat, not "just now".
func LatestSeatTime(dir, seat string) (time.Time, bool) {
	if dir == "" || seat == "" {
		return time.Time{}, false
	}
	_, ts, err := seatIndex(dir)
	if err != nil {
		return time.Time{}, false
	}
	t, ok := ts[seat]
	return t, ok
}

// Files returns dated log names, older first.
func Files(dir string) ([]string, error) {
	names, err := listDayFiles(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, n := range names {
		paths = append(paths, filepath.Join(dir, n))
	}
	return paths, nil
}

func listDayFiles(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, "events-") && strings.HasSuffix(n, ".log") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names, nil
}

// seatIndex maps a seat to the newest dated log that names it, and
// separately to the "ts" of that seat's own newest line (🎯T893: the file
// is shared, the ts field is not). The signature is file name, size, and
// mtime, so an append rebuilds and a quiet poll does not read the spool
// again.
// fileIndex is what one dated log says about its seats, as far as offset:
// the byte just past its last complete line. The spool is append-only, so
// a file that has grown is read from offset on, never from the start
// (🎯T936: a full rescan of every day file on each append took /api/agents
// past a minute once the spool reached gigabytes).
type fileIndex struct {
	offset int64
	size   int64 // the file's size when last read; offset can trail it by a half-written line
	mod    int64
	seats  map[string]bool
	ts     map[string]time.Time
	// phase is, per seat, the newest complete line that may move the
	// seat's phase fold, and tail the unterminated last line when it may
	// (🎯T1084; see phase.go).
	phase map[string]*phaseMark
	tail  *phaseMark
	// spans is, per seat, where its complete lines are (🎯T1085; see
	// seatlines.go).
	spans map[string][]seatSpan
}

func newFileIndex() *fileIndex {
	return &fileIndex{
		seats: map[string]bool{}, ts: map[string]time.Time{},
		phase: map[string]*phaseMark{}, spans: map[string][]seatSpan{},
	}
}

type indexedSeats struct {
	files map[string]*fileIndex
}

var (
	seatIndexMu sync.Mutex
	seatIndexes = map[string]*indexedSeats{}
	// scannedBytes counts spool bytes read from day files, for tests.
	scannedBytes atomic.Int64
)

// ScannedBytes is how many day-file bytes this process has read through
// the spool readers. Tests diff it across a call to prove what the call
// cost (🎯T936, 🎯T1084); it is not a production signal.
func ScannedBytes() int64 { return scannedBytes.Load() }

// countingReader adds what it reads to scannedBytes.
type countingReader struct{ r io.Reader }

func (c countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	scannedBytes.Add(int64(n))
	return n, err
}

// seatIndex maps a seat to the newest dated log that names it, and
// separately to the "ts" of that seat's own newest line (🎯T893: the file
// is shared, the ts field is not). Unchanged files are not read again, and
// a grown file is read only past what was already indexed.
func seatIndex(dir string) (map[string]string, map[string]time.Time, error) {
	seatIndexMu.Lock()
	defer seatIndexMu.Unlock()
	idx, names, err := refreshIndexLocked(dir)
	if err != nil {
		return nil, nil, err
	}
	// Date order: a later file naming a seat wins, for its path and its ts.
	hits := map[string]string{}
	ts := map[string]time.Time{}
	for _, name := range names {
		fi := idx.files[name]
		for seat := range fi.seats {
			hits[seat] = filepath.Join(dir, name)
			if t, ok := fi.ts[seat]; ok {
				ts[seat] = t
			} else {
				delete(ts, seat)
			}
		}
	}
	return hits, ts, nil
}

// refreshIndexLocked brings dir's index up to date with what is on disk and
// returns it with the day-file names, older first. A missing dir is an
// empty index. The caller holds seatIndexMu.
func refreshIndexLocked(dir string) (*indexedSeats, []string, error) {
	names, err := listDayFiles(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return &indexedSeats{files: map[string]*fileIndex{}}, nil, nil
		}
		return nil, nil, err
	}
	idx := seatIndexes[dir]
	if idx == nil {
		idx = &indexedSeats{files: map[string]*fileIndex{}}
		seatIndexes[dir] = idx
	}
	live := make(map[string]bool, len(names))
	for _, name := range names {
		live[name] = true
		path := filepath.Join(dir, name)
		st, err := os.Stat(path)
		if err != nil {
			return nil, nil, err
		}
		fi := idx.files[name]
		if fi != nil && st.Size() == fi.size && st.ModTime().UnixNano() == fi.mod {
			continue
		}
		if fi == nil || st.Size() < fi.offset {
			// New, or rewritten shorter than what was read: start over.
			fi = newFileIndex()
			idx.files[name] = fi
		}
		off, err := fi.scanFrom(path)
		if err != nil {
			return nil, nil, err
		}
		fi.offset = off
		fi.size = st.Size()
		fi.mod = st.ModTime().UnixNano()
	}
	for name := range idx.files {
		if !live[name] {
			delete(idx.files, name)
		}
	}
	return idx, names, nil
}

// scanFrom reads path from fi.offset on. Per seat it records that the file
// names it, the "ts" of its newest line there (last in file wins, since
// lines are chronological), the newest line that may move its phase
// (🎯T1084), and where its lines are (🎯T1085). Only each line's head is
// decoded, except a text line, which is small and decoded whole, and a line
// whose head does not plainly name its seat. It returns the offset just past the last complete
// line: a line still being written is read again, whole, next time, and
// until then is fi.tail.
func (fi *fileIndex) scanFrom(path string) (int64, error) {
	offset := fi.offset
	f, err := os.Open(path)
	if err != nil {
		return offset, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return offset, err
	}
	fi.tail = nil
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		head, whole, n, complete, err := nextLineHead(r)
		scannedBytes.Add(n)
		if n > 0 {
			mark := markLine(head, whole, offset, n)
			if complete {
				if seat := seatField(head); seat != "" {
					fi.seats[seat] = true
					if t, ok := tsField(head); ok {
						fi.ts[seat] = t
					}
				}
				if mark != nil {
					fi.phase[mark.seat] = mark
				}
				owner, ok := headSeat(head)
				if !ok {
					owner = lineOwner(whole) // nextLineHead kept it whole
				}
				if owner != "" {
					fi.addSpan(owner, offset, n)
				}
				offset += n
			} else {
				fi.tail = mark
			}
		}
		if err == io.EOF {
			return offset, nil
		}
		if err != nil {
			return offset, err
		}
	}
}

// headLen is how much of a line the index reads to learn its seat, ts and
// type. The sidecar writes those three first.
const headLen = 512

// nextLineHead reads one line: its first headLen bytes, the whole line when
// the head says it is a text record or does not plainly name its seat
// (headSeat), its full length, and whether it ended in a newline (a line cut
// short by EOF is still being written).
func nextLineHead(r *bufio.Reader) (head, whole []byte, n int64, complete bool, err error) {
	first, keep := true, false
	for {
		chunk, err := r.ReadSlice('\n')
		n += int64(len(chunk))
		if len(head) < headLen {
			room := headLen - len(head)
			if len(chunk) > room {
				head = append(head, chunk[:room]...)
			} else {
				head = append(head, chunk...)
			}
		}
		if first {
			// A first chunk is the whole line or a full buffer, so the
			// head is complete by now.
			first = false
			_, seated := headSeat(head)
			keep = LineType(head) == "text" || !seated
		}
		if keep {
			whole = append(whole, chunk...)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return head, whole, n, false, err
		}
		return head, whole, n, true, nil
	}
}

func seatField(prefix []byte) string {
	const key = `"seat":"`
	i := bytes.Index(prefix, []byte(key))
	if i < 0 {
		return ""
	}
	rest := prefix[i+len(key):]
	j := bytes.IndexByte(rest, '"')
	if j <= 0 {
		return ""
	}
	return string(rest[:j])
}

// tsField extracts the "ts" field from a record prefix, parsing it as
// RFC3339 (with or without sub-second precision). ok is false when the
// field is missing or unparseable — callers must not treat that as "now".
func tsField(prefix []byte) (time.Time, bool) {
	const key = `"ts":"`
	i := bytes.Index(prefix, []byte(key))
	if i < 0 {
		return time.Time{}, false
	}
	rest := prefix[i+len(key):]
	j := bytes.IndexByte(rest, '"')
	if j <= 0 {
		return time.Time{}, false
	}
	raw := string(rest[:j])
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	return time.Time{}, false
}
