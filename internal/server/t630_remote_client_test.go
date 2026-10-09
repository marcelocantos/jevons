// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

// t630Writer records Broadcast / BroadcastBinary payloads. Writes are
// themselves concurrent (a snapshot can still write after unregister), so
// the sink is mutexed independently of s.mu.
type t630Writer struct {
	mu     sync.Mutex
	text   [][]byte
	binary [][]byte
}

func (w *t630Writer) WriteText(_ context.Context, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.text = append(w.text, append([]byte(nil), data...))
	return nil
}

func (w *t630Writer) WriteBinary(_ context.Context, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.binary = append(w.binary, append([]byte(nil), data...))
	return nil
}

func (w *t630Writer) Close() error { return nil }

func (w *t630Writer) countType(typ string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, b := range w.text {
		var m map[string]any
		if json.Unmarshal(b, &m) == nil && m["type"] == typ {
			n++
		}
	}
	return n
}

func (w *t630Writer) binaryN() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.binary)
}

// 🎯T630: a client that stays registered receives every Broadcast and
// BroadcastBinary while other clients register, receive, and unregister.
// Under -race an unlocked remotes map fails this test.
func TestT630RemoteClientRegisterRemoveBroadcast(t *testing.T) {
	s := New("test", t.TempDir())
	stable := &t630Writer{}
	id, n := s.registerRemote(remoteConn{writer: stable, ctx: context.Background()})
	if n != 1 {
		t.Fatalf("stable register clients=%d want 1", n)
	}
	defer s.unregisterRemote(id)

	const broadcasts = 100
	const churners = 8
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(churners)
	for range churners {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					w := &t630Writer{}
					cid, _ := s.registerRemote(remoteConn{writer: w, ctx: context.Background()})
					s.Broadcast(map[string]any{"type": "churn"})
					s.unregisterRemote(cid)
				}
			}
		}()
	}

	for i := range broadcasts {
		s.Broadcast(map[string]any{"type": "tick", "n": i})
		s.BroadcastBinary([]byte{byte(i)})
	}
	close(stop)
	wg.Wait()

	if got := stable.countType("tick"); got != broadcasts {
		t.Fatalf("stable client got %d tick frames, want %d", got, broadcasts)
	}
	if got := stable.binaryN(); got != broadcasts {
		t.Fatalf("stable client got %d binary frames, want %d", got, broadcasts)
	}
	if left := len(s.snapshotRemotes()); left != 1 {
		t.Fatalf("after churn remotes=%d want 1 (the stable client)", left)
	}
}
