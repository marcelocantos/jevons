// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

// 🎯T1302: the shards of a run are disjoint and together are every selected
// journey, so splitting the suite drops none and runs none twice.
func TestT1302ShardsPartitionTheSuite(t *testing.T) {
	names := []string{"J1-health", "J2-chat-round-trip", "J32-overseer-overlap-queue", "J3-cancel-and-send",
		"J32-packaged-react-send-cutin", "J45-t1091-owner-after-recover", "J38-midturn-answer", "J39-t952-hold",
		"J22-send-once", "J23-fold-md", "J18-t418-mute"}
	for count := 1; count <= 4; count++ {
		of := assignShards(names, count)
		seen := map[string]int{}
		for i := 1; i <= count; i++ {
			s := &suite{shardIndex: i, shardCount: count, shardOf: of}
			for _, n := range names {
				if s.inShard(n) {
					seen[n]++
				}
			}
		}
		for _, n := range names {
			want := 1
			if everyShard[n] {
				want = count
			}
			if seen[n] != want {
				t.Errorf("%d shards: %s runs %d time(s), want %d", count, n, seen[n], want)
			}
		}
		if again := assignShards(names, count); len(again) != len(of) {
			t.Fatalf("%d shards: assignment not deterministic", count)
		} else {
			for n, i := range of {
				if again[n] != i {
					t.Errorf("%d shards: %s moved from shard %d to %d between runs", count, n, i, again[n])
				}
			}
		}
	}
	// The two longest journeys do not share a shard when there are two.
	of := assignShards(names, 2)
	if of["J32-packaged-react-send-cutin"] == of["J45-t1091-owner-after-recover"] {
		t.Errorf("J32 send-cutin and J45 landed in the same shard of two: %v", of)
	}
}

func TestT1302ParseShard(t *testing.T) {
	for spec, ok := range map[string]bool{"": true, "1/3": true, "3/3": true, "0/3": false, "4/3": false, "1/0": false, "x/3": false, "2": false} {
		if _, _, err := parseShard(spec); (err == nil) != ok {
			t.Errorf("parseShard(%q): err=%v, want ok=%v", spec, err, ok)
		}
	}
}
