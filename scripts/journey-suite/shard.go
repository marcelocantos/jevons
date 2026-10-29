// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Journey shards (🎯T1302). A Jevons instance has one overseer and one owner
// conversation, and most journeys drive that path, so within an isolate they
// run one at a time. Several suites, each with its own isolate and so its own
// overseer, run the overseer journeys side by side: -shard i/N runs the i-th
// of N disjoint subsets. J1 (health) and J5 (isolation) are each shard's own
// precondition and postcondition and run in every shard.

// journeySeconds is each journey's measured duration on openai-codex (fast
// gate a21b75ed, 2026-10-29; J22-J28 dropped from 27 s to ~3 s with 🎯T1303
// and now weigh the default), for balancing shards by time rather than count.
// A journey missing here weighs defaultJourneySeconds. The numbers only steer
// the split; a stale one makes shards uneven, never wrong.
var journeySeconds = map[string]float64{
	"J32-packaged-react-send-cutin": 195, "J45-t1091-owner-after-recover": 120,
	"J38-midturn-answer": 86, "J39-t952-hold": 65, "J37-busy-escalation": 57,
	"J34-hot-provider-switch": 30, "J13-overseer-switch": 21, "J12-provider-switch": 21,
	"J30-packaged-react-owner-turn": 22, "J16-stopped-provider-switch": 18,
	"J46-t4043-owner-question-route": 18, "J47-t1140-owner-reply-wire": 20,
	"J52-t1120-one-answer": 23, "J17-t418-queue-bounce": 15, "J31-packaged-react-owner-boundary": 15,
	"J49-t736-scout-handoff": 15, "J3-cancel-and-send": 14, "J20-plan-dest": 13,
	"J32-overseer-overlap-queue": 12, "J21-goal-continue-all-backends": 12,
	"J14-bounce-resume": 10,
}

const defaultJourneySeconds = 5

// everyShard are journeys every shard runs.
var everyShard = map[string]bool{"J1-health": true, "J5-isolation": true}

// parseShard reads "i/N" (1-based). The zero value means unsharded.
func parseShard(spec string) (index, count int, err error) {
	if strings.TrimSpace(spec) == "" {
		return 0, 0, nil
	}
	a, b, ok := strings.Cut(spec, "/")
	if ok {
		index, err = strconv.Atoi(a)
		if err == nil {
			count, err = strconv.Atoi(b)
		}
	}
	if !ok || err != nil || count < 1 || index < 1 || index > count {
		return 0, 0, fmt.Errorf("-shard %q: want i/N with 1 <= i <= N", spec)
	}
	return index, count, nil
}

// assignShards gives each journey in names a shard in 1..count, longest
// first onto the least-loaded shard (ties to the lower shard), so the split
// is deterministic and balanced by expected time. Callers run each shard's
// journeys in the suite's own order, which ordering constraints rely on
// (J32 before J3, J18 last).
func assignShards(names []string, count int) map[string]int {
	type job struct {
		name string
		secs float64
		pos  int
	}
	var jobs []job
	for i, n := range names {
		if everyShard[n] {
			continue
		}
		secs, ok := journeySeconds[n]
		if !ok {
			secs = defaultJourneySeconds
		}
		jobs = append(jobs, job{n, secs, i})
	}
	sort.SliceStable(jobs, func(a, b int) bool {
		if jobs[a].secs != jobs[b].secs {
			return jobs[a].secs > jobs[b].secs
		}
		return jobs[a].pos < jobs[b].pos
	})
	load := make([]float64, count)
	out := make(map[string]int, len(names))
	for _, j := range jobs {
		best := 0
		for k := 1; k < count; k++ {
			if load[k] < load[best] {
				best = k
			}
		}
		load[best] += j.secs
		out[j.name] = best + 1
	}
	return out
}

// inShard reports whether this suite's shard runs name.
func (s *suite) inShard(name string) bool {
	if s.shardCount == 0 || everyShard[name] {
		return true
	}
	return s.shardOf[name] == s.shardIndex
}
