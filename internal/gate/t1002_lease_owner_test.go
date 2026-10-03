// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func fixtureWait(path string) {
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// These are private subprocess fixtures, never the development lease/processes.
func TestT1002LeaseProcess(t *testing.T) {
	mode := os.Getenv("T1002_MODE")
	if mode == "" {
		return
	}
	root := os.Getenv("T1002_ROOT")
	switch mode {
	case "wrapper":
		_ = os.WriteFile(filepath.Join(root, "wrapper-ready"), nil, 0600)
		fixtureWait(filepath.Join(root, "wrapper-exit"))
		_ = os.WriteFile(filepath.Join(root, "wrapper-exited"), nil, 0600)
	case "owner":
		release, token, err := acquireHeavyLease(root, "")
		if err != nil {
			panic(err)
		}
		defer release()
		wrapper := exec.Command(os.Args[0], "-test.run=^TestT1002LeaseProcess$")
		wrapper.Env = append(os.Environ(), "T1002_MODE=wrapper", heavyLeaseEnv+"="+token)
		if os.Getenv("T1002_LEGACY") == "1" {
			// Reproduce a retained description exactly: duplicate the locked FD,
			// rather than opening an unrelated file description on the same inode.
			// The controlled fixture first replaces the lease with a legacy holder.
			release()
			f, err := os.OpenFile(heavyLockPath(root), os.O_RDWR, 0600)
			if err != nil {
				panic(err)
			}
			if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
				panic(err)
			}
			wrapper.ExtraFiles = []*os.File{f}
		}
		if err := wrapper.Start(); err != nil {
			panic(err)
		}
		fixtureWait(filepath.Join(root, "wrapper-ready"))
		_ = os.WriteFile(filepath.Join(root, "owner-ready"), nil, 0600)
		fixtureWait(filepath.Join(root, "owner-exit"))
		// Model abrupt exit: neither deferred unlock nor listener close executes.
		os.Exit(0)
	case "waiter", "stale-waiter":
		token, err := os.ReadFile(heavyLockPath(root))
		if err != nil {
			panic(err)
		}
		if mode == "waiter" {
			token = nil
		}
		_ = os.WriteFile(filepath.Join(root, "waiter-ready"), nil, 0600)
		release, _, err := acquireHeavyLease(root, string(token))
		if err != nil {
			panic(err)
		}
		defer release()
		_ = os.WriteFile(filepath.Join(root, "acquired"), nil, 0600)
	}
}

func awaitFixture(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("fixture did not produce %s", path)
}

func TestT1002WrapperCannotRetainNewLease(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		t.Run(map[bool]string{true: "legacy-retention-and-stale-token", false: "current-no-descriptor-handoff"}[legacy], func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			start := func(mode string) *exec.Cmd {
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestT1002LeaseProcess$")
				cmd.Env = append(os.Environ(), "T1002_MODE="+mode, "T1002_ROOT="+root)
				if legacy {
					cmd.Env = append(cmd.Env, "T1002_LEGACY=1")
				}
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				return cmd
			}
			// Always release the private surviving wrapper, including assertion failure.
			defer func() {
				_ = os.WriteFile(filepath.Join(root, "wrapper-exit"), nil, 0600)
				awaitFixture(t, filepath.Join(root, "wrapper-exited"))
			}()
			owner := start("owner")
			awaitFixture(t, filepath.Join(root, "owner-ready"))
			var waiter *exec.Cmd
			if !legacy {
				waiter = start("waiter")
				awaitFixture(t, filepath.Join(root, "waiter-ready"))
				time.Sleep(200 * time.Millisecond)
				if _, err := os.Stat(filepath.Join(root, "acquired")); err == nil {
					t.Fatal("independent waiter bypassed live holder")
				}
			}
			if err := os.WriteFile(filepath.Join(root, "owner-exit"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := owner.Wait(); err != nil {
				t.Fatal(err)
			}
			token, err := os.ReadFile(heavyLockPath(root))
			if err != nil {
				t.Fatal(err)
			}
			if leaseOwnerAlive(string(token)) {
				t.Fatal("dead owner's token still authorizes nesting")
			}
			if legacy {
				waiter = start("stale-waiter")
				awaitFixture(t, filepath.Join(root, "waiter-ready"))
				time.Sleep(200 * time.Millisecond)
				if _, err := os.Stat(filepath.Join(root, "acquired")); err == nil {
					t.Fatal("stale token bypassed retained descriptor")
				}
				// Only the deliberately stranded private fixture is released, by exiting
				// its wrapper. Production must never force-unlock this shape.
				if err := os.WriteFile(filepath.Join(root, "wrapper-exit"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			awaitFixture(t, filepath.Join(root, "acquired"))
			if err := waiter.Wait(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestT1002OwnerVisibility(t *testing.T) {
	root := t.TempDir()
	release, token, err := acquireHeavyLeaseCommand(root, "", []string{"make", "test-heavy"}, "/fixture/cwd")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if !leaseOwnerAlive(token) {
		t.Fatal("active gate responder missing")
	}
	var out bytes.Buffer
	if err := WriteHeavyLeaseStatus(&out, root); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"responder_alive=true", "test-heavy", "/fixture/cwd", "acquisition_age=", "open descriptors"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
}
