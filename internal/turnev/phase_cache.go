// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turnev

import (
	"bufio"
	"io"
	"os"
	"sync"
)

// 🎯T705: classifying a seat's phase costs what was appended, not what the
// file weighs.
//
// ClassifyPhaseFile used to open the transcript and decode every event from
// byte zero, and the idle loop calls it for every seat every 30 seconds. The
// live transcripts are megabytes and grow all night, so the cost per tick
// climbed until a tick took about as long as its own interval: jevonsd sat
// at 200% CPU on 2026-09-20, and the loops that decide whether a seat is
// stuck were themselves running late. Nothing was incremental — the daemon
// re-read all of history to learn the last thing that happened.
//
// A tail read would have been simpler and wrong. ClassifyPhase is a fold:
// `pending` counts enqueues against drains across the whole tape, so a
// window that starts after an undrained enqueue reports idle for a seat
// whose queue has been pinned for hours — exactly the state the fleet most
// needs to see. So the fold is resumed rather than restarted: its state is
// cached against the file's size and mtime, and only the bytes appended
// since are decoded.
//
// The cache is keyed on append-only semantics. A transcript that shrinks, or
// whose mtime moves backwards, is re-read in full — that is a rotated or
// rewritten file, not an appended one.

// phaseFold is ClassifyPhase's state, exposed so it can be carried across
// calls. pending and last mean exactly what they mean in ClassifyPhase.
type phaseFold struct {
	pending int
	last    Phase
}

// phaseCacheEntry is a fold paused at a byte offset in one file.
type phaseCacheEntry struct {
	// offset is the end of the last *complete* line consumed. A writer
	// appending mid-line must not cause that line to be skipped when the
	// rest of it arrives, so a partial tail is never counted.
	offset int64
	modNs  int64
	fold   phaseFold
}

// phaseCacheMax bounds the map: one entry per transcript the daemon has
// classified. A fleet has tens of seats, not thousands, and a stale entry
// costs one full re-read rather than a wrong answer.
const phaseCacheMax = 512

var (
	phaseCacheMu sync.Mutex
	phaseCache   = map[string]phaseCacheEntry{}
)

// ResetPhaseCache drops all cached folds. Tests use it to force the
// full-read path; production never needs it.
func ResetPhaseCache() {
	phaseCacheMu.Lock()
	defer phaseCacheMu.Unlock()
	phaseCache = map[string]phaseCacheEntry{}
}

// classifyFile is the incremental implementation behind ClassifyPhaseFile.
func classifyFile(path string) Phase {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return PhaseUnknown
	}
	size, modNs := st.Size(), st.ModTime().UnixNano()

	phaseCacheMu.Lock()
	entry, cached := phaseCache[path]
	phaseCacheMu.Unlock()

	// Resume only from an append. A shrunken file, or one whose mtime went
	// backwards, is a different tape wearing the same name.
	resume := cached && size >= entry.offset && modNs >= entry.modNs
	if !resume {
		entry = phaseCacheEntry{}
	}
	if resume && size == entry.offset {
		// Nothing appended: the previous answer still stands, for a stat.
		return entry.fold.phase()
	}

	f, err := os.Open(path)
	if err != nil {
		return PhaseUnknown
	}
	defer f.Close()
	if entry.offset > 0 {
		if _, err := f.Seek(entry.offset, io.SeekStart); err != nil {
			// Fall back to the whole file rather than a wrong window.
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return PhaseUnknown
			}
			entry = phaseCacheEntry{}
		}
	}

	fold := entry.fold   // cached: advances only over complete lines
	answer := entry.fold // this call's reading, may include a partial tail
	consumed := entry.offset
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), scanMax)
	for sc.Scan() {
		line := sc.Bytes()
		rec, ok := Decode(line)
		// A line is complete when a newline followed it, which is true
		// whenever it ended before the file did. The final line of a file
		// with no trailing newline is a writer caught mid-append.
		if consumed+int64(len(line)) >= size {
			// Count it in this answer so a just-written event is not
			// invisible, but never in the cached fold: the completed line
			// will be read again next time, and counting it twice would
			// leave `pending` permanently wrong.
			if ok {
				answer.step(rec)
			}
			break
		}
		if ok {
			fold.step(rec)
			answer.step(rec)
		}
		consumed += int64(len(line)) + 1
	}

	phaseCacheMu.Lock()
	if len(phaseCache) >= phaseCacheMax {
		phaseCache = map[string]phaseCacheEntry{}
	}
	phaseCache[path] = phaseCacheEntry{offset: consumed, modNs: modNs, fold: fold}
	phaseCacheMu.Unlock()

	return answer.phase()
}
