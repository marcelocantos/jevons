// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// restartauth is an accidental-misuse gate, NOT a security boundary between
// same-UID processes. A worker can bypass it by directly signalling the daemon
// or writing its artifacts; the deployment model trusts a single operator.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/marcelocantos/claudia"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var auto = regexp.MustCompile(`^jv-t[0-9]+(\.[0-9]+)*-auto$`)
var sha = regexp.MustCompile(`^[0-9a-f]{40}$`)

type permit struct {
	ID      string    `json:"id"`
	Seat    string    `json:"seat"`
	Target  string    `json:"target"`
	Action  string    `json:"action"`
	Base    string    `json:"base"`
	Head    string    `json:"head"`
	Issuer  string    `json:"issuer"`
	Expires time.Time `json:"expires"`
}

func git(root string, args ...string) (string, error) {
	c := exec.Command("git", append([]string{"-C", root}, args...)...)
	b, e := c.Output()
	return strings.TrimSpace(string(b)), e
}
func state() string {
	if s := os.Getenv("JEVONS_STATE_DIR"); s != "" {
		return s
	}
	return filepath.Join(os.Getenv("HOME"), ".jevons")
}
func audit(dir, event string, p permit) error {
	f, e := os.OpenFile(filepath.Join(dir, "audit.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(struct {
		Event  string    `json:"event"`
		At     time.Time `json:"at"`
		Permit permit    `json:"permit"`
	}{event, time.Now().UTC(), p})
}
func run(args []string) error {
	if len(args) < 2 {
		return errors.New("usage: restartauth grant <repo-root> <seat> <target> <base> <head> | check <root>")
	}
	dir := filepath.Join(state(), "restart-auth")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	switch args[0] {
	case "grant":
		if len(args) != 6 {
			return errors.New("grant requires repo-root seat target base head")
		}
		issuer := os.Getenv("JEVONS_AGENT")
		if issuer == "" || auto.MatchString(issuer) {
			return errors.New("grant requires named PO/overseer identity (not auto-frontier)")
		}
		seat, target, base, head := args[2], args[3], args[4], args[5]
		registry, err := claudia.NewRegistry(filepath.Join(state(), "agents.json"))
		if err != nil {
			return fmt.Errorf("host registry unavailable: %w", err)
		}
		var issuerOK, seatOK bool
		for _, agent := range registry.List() {
			if agent.Name == issuer && (agent.Role == "product-owner" || agent.Role == "overseer") {
				issuerOK = true
			}
			if agent.Name == seat && (agent.Role == "worker" || agent.Role == "") && agent.TargetID == target {
				seatOK = true
			}
		}
		if !issuerOK || !seatOK {
			return errors.New("host registry does not verify issuer role and target seat")
		}
		actualHead, err := git(args[1], "rev-parse", "HEAD")
		if err != nil || actualHead != head {
			return errors.New("grant head does not match repo")
		}
		if _, err = git(args[1], "merge-base", "--is-ancestor", base, head); err != nil {
			return errors.New("grant base is not ancestor of head")
		}
		if !auto.MatchString(seat) || !regexp.MustCompile(`^T[0-9]+(\.[0-9]+)*$`).MatchString(target) || !sha.MatchString(base) || !sha.MatchString(head) {
			return errors.New("invalid permit scope")
		}
		if strings.ToLower("jv-"+target+"-auto") != seat {
			return errors.New("seat does not match target")
		}
		var id [16]byte
		if _, e := rand.Read(id[:]); e != nil {
			return e
		}
		p := permit{ID: hex.EncodeToString(id[:]), Seat: seat, Target: target, Action: "restart-development", Base: base, Head: head, Issuer: issuer, Expires: time.Now().UTC().Add(10 * time.Minute)}
		b, e := json.Marshal(p)
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(dir, p.ID+".json"), b, 0600); e != nil {
			return e
		}
		if e = audit(dir, "grant", p); e != nil {
			return e
		}
		fmt.Println(p.ID)
		return nil
	case "check":
		if len(args) != 2 {
			return errors.New("check requires repository root")
		}
		seat := os.Getenv("JEVONS_AGENT")
		if !auto.MatchString(seat) {
			return nil
		}
		id := os.Getenv("JEVONS_RESTART_AUTH_ID")
		if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(id) {
			return errors.New("auto-frontier restart requires fresh scoped host permit (JEVONS_RESTART_AUTH_ID)")
		}
		b, e := os.ReadFile(filepath.Join(dir, id+".json"))
		if e != nil {
			return fmt.Errorf("permit unavailable: %w", e)
		}
		var p permit
		if e = json.Unmarshal(b, &p); e != nil {
			return e
		}
		if p.ID != id || p.Seat != seat || p.Target != "T"+strings.TrimSuffix(strings.TrimPrefix(seat, "jv-t"), "-auto") || p.Action != "restart-development" || time.Now().After(p.Expires) || time.Until(p.Expires) > 10*time.Minute || !sha.MatchString(p.Base) || !sha.MatchString(p.Head) {
			return errors.New("permit expired or scope mismatch")
		}
		head, e := git(args[1], "rev-parse", "HEAD")
		if e != nil || head != p.Head {
			return errors.New("permit head does not match checkout")
		}
		if _, e = git(args[1], "merge-base", "--is-ancestor", p.Base, p.Head); e != nil {
			return errors.New("permit base is not ancestor of head")
		}
		if e = audit(dir, "accepted", p); e != nil {
			return fmt.Errorf("audit write failed: %w", e)
		}
		fmt.Fprintf(os.Stderr, "restartauth: accepted permit=%s seat=%s target=%s action=%s range=%s..%s issuer=%s (NOT complete containment: same-UID processes can directly signal or write daemon artifacts)\n", id, seat, p.Target, p.Action, p.Base, p.Head, p.Issuer)
		return nil
	default:
		return errors.New("unknown restartauth operation")
	}
}
func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, "restartauth: refusing:", e, "(NOT complete containment: same-UID processes can directly signal or write daemon artifacts)")
		os.Exit(3)
	}
}
