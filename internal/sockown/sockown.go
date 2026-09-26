// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package sockown is how jevons-broker takes exclusive ownership of the
// host broker socket (🎯T872). Claudia's Listen refuses to steal a live
// socket; this package evicts the foreign listener first so that refusal
// never happens.
package sockown

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// SocketPathEnv is the same override Claudia uses. One path on the host.
const SocketPathEnv = "CLAUDIA_BROKER_SOCKET"

// DefaultPath is the socket jevons-broker binds. Empty SocketPathEnv
// means ~/.local/state/claudia/broker.sock (or $XDG_STATE_HOME/claudia).
func DefaultPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv(SocketPathEnv)); override != "" {
		return filepath.Abs(override)
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); xdg != "" {
		return filepath.Abs(filepath.Join(xdg, "claudia", "broker.sock"))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("sockown: home: %w", err)
	}
	return filepath.Join(home, ".local", "state", "claudia", "broker.sock"), nil
}

// Homebrew and owner-installed Claudia daemons that used to bind the
// same path. KeepAlive on either will steal the socket the moment we
// SIGTERM the process unless the job is also unloaded.
const (
	BrewClaudiaLabel  = "sh.brew.claudia"
	OwnerClaudiaLabel = "com.marcelocantos.claudia-broker"
)

// Listener is one process holding a Unix socket path.
type Listener struct {
	PID int
	Cmd string
}

// ClaimArgs is the takeover. Tests inject List, Kill, Bootout, and Wait.
type ClaimArgs struct {
	Socket  string
	SelfPID int
	List    func(socket string) ([]Listener, error)
	Kill    func(pid int, sig syscall.Signal) error
	Bootout func(label string) error
	Wait    func()
}

// Claim makes Socket free for this process to bind. It unloads the
// known Claudia launchd jobs, then SIGTERMs any other holder.
func Claim(a ClaimArgs) error {
	if strings.TrimSpace(a.Socket) == "" {
		return fmt.Errorf("sockown: empty socket path")
	}
	self := a.SelfPID
	if self == 0 {
		self = os.Getpid()
	}
	list := a.List
	if list == nil {
		list = ListLSOF
	}
	kill := a.Kill
	if kill == nil {
		kill = func(pid int, sig syscall.Signal) error {
			return syscall.Kill(pid, sig)
		}
	}
	bootout := a.Bootout
	if bootout == nil {
		bootout = bootoutLaunchd
	}
	wait := a.Wait
	if wait == nil {
		wait = func() { time.Sleep(200 * time.Millisecond) }
	}

	for _, label := range []string{BrewClaudiaLabel, OwnerClaudiaLabel} {
		_ = bootout(label)
	}

	holders, err := list(a.Socket)
	if err != nil {
		return err
	}
	var foreign []Listener
	for _, h := range holders {
		if h.PID == 0 || h.PID == self {
			continue
		}
		foreign = append(foreign, h)
	}
	if len(foreign) == 0 {
		return nil
	}
	for _, h := range foreign {
		if err := kill(h.PID, syscall.SIGTERM); err != nil {
			return fmt.Errorf("sockown: SIGTERM %s pid %d: %w", h.Cmd, h.PID, err)
		}
	}
	for i := 0; i < 15; i++ {
		wait()
		holders, err = list(a.Socket)
		if err != nil {
			return err
		}
		left := 0
		for _, h := range holders {
			if h.PID != 0 && h.PID != self {
				left++
			}
		}
		if left == 0 {
			break
		}
		if i == 14 {
			for _, h := range holders {
				if h.PID != 0 && h.PID != self {
					_ = kill(h.PID, syscall.SIGKILL)
				}
			}
			wait()
		}
	}
	return nil
}

// Owned reports whether the only live holder of socket is a jevons-broker
// process (this product). An empty socket with no holder is not owned.
func Owned(socket string, list func(string) ([]Listener, error)) (bool, error) {
	if list == nil {
		list = ListLSOF
	}
	holders, err := list(socket)
	if err != nil {
		return false, err
	}
	if len(holders) == 0 {
		return false, nil
	}
	for _, h := range holders {
		if h.PID == 0 {
			continue
		}
		if !isJevonsBroker(h.Cmd) {
			return false, nil
		}
	}
	return true, nil
}

func isJevonsBroker(cmd string) bool {
	base := cmd
	if i := strings.LastIndex(cmd, "/"); i >= 0 {
		base = cmd[i+1:]
	}
	return base == "jevons-broker"
}

func bootoutLaunchd(label string) error {
	uid := strconv.Itoa(os.Getuid())
	domain := "gui/" + uid
	target := domain + "/" + label
	bin, err := exec.LookPath("launchctl")
	if err != nil {
		return err
	}
	_ = exec.Command(bin, "bootout", target).Run()
	_ = exec.Command(bin, "disable", target).Run()
	return nil
}
