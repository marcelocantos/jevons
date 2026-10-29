// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eventlog

import (
	"fmt"
	"os"
)

// DefaultMaxSegmentBytes is the live journal size before rotation (🎯T1131).
const DefaultMaxSegmentBytes = 50 << 20

// DefaultKeepSegments is how many files to retain: the live segment plus
// archived .1 … .(KeepSegments-1).
const DefaultKeepSegments = 5

// SegmentPaths returns journal files newest-first: live path, then .1, .2, …
func SegmentPaths(live string) ([]string, error) {
	if live == "" {
		return nil, fmt.Errorf("eventlog: live path required")
	}
	out := []string{live}
	for i := 1; i < 256; i++ {
		p := segmentPath(live, i)
		if _, err := os.Stat(p); err != nil {
			if os.IsNotExist(err) {
				break
			}
			return nil, fmt.Errorf("eventlog: stat segment %q: %w", p, err)
		}
		out = append(out, p)
	}
	return out, nil
}

func segmentPath(live string, n int) string {
	return fmt.Sprintf("%s.%d", live, n)
}

func (j *Journal) maybeRotate() error {
	if j == nil || j.f == nil || j.maxSegmentBytes <= 0 || j.keepSegments < 2 {
		return nil
	}
	info, err := j.f.Stat()
	if err != nil {
		return fmt.Errorf("eventlog: stat before rotate: %w", err)
	}
	if info.Size() < j.maxSegmentBytes {
		return nil
	}
	return j.rotateLocked()
}

func (j *Journal) rotateLocked() error {
	path := j.path
	if err := j.f.Sync(); err != nil {
		return fmt.Errorf("eventlog: sync before rotate: %w", err)
	}
	if err := j.f.Close(); err != nil {
		return fmt.Errorf("eventlog: close before rotate: %w", err)
	}
	j.f = nil

	oldest := segmentPath(path, j.keepSegments-1)
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("eventlog: drop oldest segment: %w", err)
	}
	for i := j.keepSegments - 2; i >= 1; i-- {
		from := segmentPath(path, i)
		to := segmentPath(path, i+1)
		if _, err := os.Stat(from); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("eventlog: stat segment %q: %w", from, err)
		}
		if err := os.Rename(from, to); err != nil {
			return fmt.Errorf("eventlog: shift segment %q: %w", from, err)
		}
	}
	archived := segmentPath(path, 1)
	if err := os.Rename(path, archived); err != nil {
		return fmt.Errorf("eventlog: archive live segment: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("eventlog: open after rotate: %w", err)
	}
	j.f = f
	return nil
}
