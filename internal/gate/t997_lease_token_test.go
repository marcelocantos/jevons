// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"os"
	"syscall"
	"testing"
	"time"
)

func TestT997NestedReleaseKeepsOuterLock(t *testing.T) {
	root := t.TempDir()
	release, token, err := acquireHeavyLease(root, "")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	innerRelease, innerToken, err := acquireHeavyLease(root, token)
	if err != nil {
		t.Fatal(err)
	}
	if innerToken != token {
		t.Fatal("nested lease changed owner")
	}
	innerRelease()
	f, err := os.OpenFile(heavyLockPath(root), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
		t.Fatalf("outer lock not held after inner release: %v", err)
	}
}

func TestT997InvalidTokensStillQueue(t *testing.T) {
	for _, kind := range []string{"absent", "forged", "stale", "other-store"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			token := ""
			switch kind {
			case "forged":
				token = "pretend-held"
			case "stale", "other-store":
				oldRoot := root
				if kind == "other-store" {
					oldRoot = t.TempDir()
				}
				release, oldToken, err := acquireHeavyLease(oldRoot, "")
				if err != nil {
					t.Fatal(err)
				}
				token = oldToken
				release()
			}
			release, err := AcquireHeavyLease(root)
			if err != nil {
				t.Fatal(err)
			}
			acquired := make(chan error, 1)
			go func() {
				r, _, err := acquireHeavyLease(root, token)
				if err == nil {
					r()
				}
				acquired <- err
			}()
			select {
			case err := <-acquired:
				release()
				t.Fatalf("%s token bypassed held lease: %v", kind, err)
			case <-time.After(100 * time.Millisecond):
			}
			release()
			select {
			case err := <-acquired:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("lease stayed blocked after release")
			}
		})
	}
}
