// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sockown

import (
	"syscall"
	"testing"
)

func TestT872ClaimEvictsForeignListener(t *testing.T) {
	var killed []int
	var sigs []syscall.Signal
	var bootout []string
	holders := []Listener{{PID: 3579, Cmd: "claudia"}}
	err := Claim(ClaimArgs{
		Socket:  "/tmp/broker.sock",
		SelfPID: 100,
		List: func(string) ([]Listener, error) {
			return holders, nil
		},
		Kill: func(pid int, sig syscall.Signal) error {
			killed = append(killed, pid)
			sigs = append(sigs, sig)
			holders = nil
			return nil
		},
		Bootout: func(label string) error {
			bootout = append(bootout, label)
			return nil
		},
		Wait: func() {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(killed) != 1 || killed[0] != 3579 {
		t.Fatalf("killed %v", killed)
	}
	if sigs[0] != syscall.SIGTERM {
		t.Fatalf("sig %v", sigs)
	}
	want := map[string]bool{BrewClaudiaLabel: true, OwnerClaudiaLabel: true}
	for _, l := range bootout {
		delete(want, l)
	}
	if len(want) != 0 {
		t.Fatalf("missing bootout %v (got %v)", want, bootout)
	}
}

func TestT872ClaimLeavesSelfAlone(t *testing.T) {
	var killed []int
	err := Claim(ClaimArgs{
		Socket:  "/tmp/broker.sock",
		SelfPID: 42,
		List: func(string) ([]Listener, error) {
			return []Listener{{PID: 42, Cmd: "jevons-broker"}}, nil
		},
		Kill: func(pid int, sig syscall.Signal) error {
			killed = append(killed, pid)
			return nil
		},
		Bootout: func(string) error { return nil },
		Wait:    func() {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(killed) != 0 {
		t.Fatalf("killed self: %v", killed)
	}
}

func TestT872Owned(t *testing.T) {
	ok, err := Owned("/tmp/broker.sock", func(string) ([]Listener, error) {
		return []Listener{{PID: 1, Cmd: "jevons-broker"}}, nil
	})
	if err != nil || !ok {
		t.Fatalf("owned=%v err=%v", ok, err)
	}
	ok, err = Owned("/tmp/broker.sock", func(string) ([]Listener, error) {
		return []Listener{{PID: 1, Cmd: "claudia"}}, nil
	})
	if err != nil || ok {
		t.Fatalf("claudia must not count as owned: owned=%v err=%v", ok, err)
	}
	ok, err = Owned("/tmp/broker.sock", func(string) ([]Listener, error) {
		return nil, nil
	})
	if err != nil || ok {
		t.Fatalf("empty socket is not owned: owned=%v err=%v", ok, err)
	}
}

func TestParseLSOF(t *testing.T) {
	got := parseLSOF("p3579\ncclaudia\n")
	if len(got) != 1 || got[0].PID != 3579 || got[0].Cmd != "claudia" {
		t.Fatalf("%+v", got)
	}
}
