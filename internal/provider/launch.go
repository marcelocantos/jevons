// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
)

// Launcher starts a launch-mode provider process. This is the generic,
// non-Claude supervisor 🎯T27.3 requires: claudia's AgentDef path is
// hardwired to a Claude-family CLI (tmux PTY, --resume, LookPath of the
// agent binary) and cannot host an arbitrary argv, so launch mode gets
// its own plain os/exec supervisor rather than an extension of claudia.
//
// The interface exists so tests can substitute a fake process without
// spawning anything, and so a future transport (containers, launchd)
// slots in without touching the reconciler.
type Launcher interface {
	Start(ctx context.Context, d config.ProviderDecl) (Process, error)
}

// Process is one running provider. Wait blocks until exit; Stop
// terminates and reaps it. Both must be safe to call once.
type Process interface {
	Wait() error
	Stop(ctx context.Context) error
	PID() int
}

// ErrUnrunnable marks a declaration that no restart can fix (missing
// binary, empty argv). The runner reports PhaseFailed instead of
// looping on backoff forever.
var ErrUnrunnable = errors.New("provider is unrunnable as declared")

// StopGrace is how long a terminating provider gets to exit on SIGTERM
// before the supervisor escalates to SIGKILL.
const StopGrace = 5 * time.Second

// ExecLauncher spawns providers with os/exec in their own process group,
// so Stop reaps the provider's children too rather than orphaning them.
type ExecLauncher struct {
	// Env is the environment for launched providers. Nil inherits the
	// daemon's environment.
	Env []string
	// Dir is the working directory. Empty inherits the daemon's.
	Dir string
}

// Argv resolves a declaration to the command line to run. params.exec is
// the binary and params.argv its arguments; a bare argv list (no exec)
// is also accepted, taking argv[0] as the binary.
func Argv(d config.ProviderDecl) ([]string, error) {
	exe := strings.TrimSpace(d.LaunchExec())
	args := d.LaunchArgv()
	if exe == "" {
		if len(args) == 0 {
			return nil, fmt.Errorf("%w: provider %q has neither params.exec nor params.argv", ErrUnrunnable, d.ID)
		}
		return args, nil
	}
	return append([]string{exe}, args...), nil
}

// Start resolves argv, verifies the binary exists, and spawns the process.
func (l *ExecLauncher) Start(ctx context.Context, d config.ProviderDecl) (Process, error) {
	argv, err := Argv(d)
	if err != nil {
		return nil, err
	}
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return nil, fmt.Errorf("%w: provider %q: %v", ErrUnrunnable, d.ID, err)
	}

	// Not exec.CommandContext: cancellation must run our Stop path
	// (group SIGTERM, grace, SIGKILL), not the default bare Kill.
	cmd := exec.Command(bin, argv[1:]...)
	cmd.Dir = l.Dir
	cmd.Env = l.Env
	cmd.Stdin = nil
	cmd.Stdout = os.Stderr // provider chatter joins the daemon log stream
	cmd.Stderr = os.Stderr
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("provider %q: start: %w", d.ID, err)
	}
	return &execProcess{cmd: cmd}, nil
}

// execProcess adapts *exec.Cmd to Process.
type execProcess struct {
	cmd *exec.Cmd
}

func (p *execProcess) PID() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *execProcess) Wait() error { return p.cmd.Wait() }

// Stop signals the whole process group, then escalates to a hard kill if
// the provider has not exited by ctx deadline or StopGrace, whichever is
// sooner. It returns once the process is reaped.
func (p *execProcess) Stop(ctx context.Context) error {
	if p.cmd.Process == nil {
		return nil
	}
	terminateGroup(p.cmd.Process.Pid)

	grace := StopGrace
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline); d < grace {
			grace = d
		}
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()

	// Wait is owned by the runner goroutine, which closes exited when the
	// process is reaped; polling the signal is enough to decide escalation.
	select {
	case <-timer.C:
		killGroup(p.cmd.Process.Pid)
	case <-ctx.Done():
		killGroup(p.cmd.Process.Pid)
	case <-processDone(p.cmd):
	}
	return nil
}

// processDone yields a channel closed once the process has been reaped by
// whoever owns Wait. cmd.ProcessState is only set after Wait returns.
func processDone(cmd *exec.Cmd) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		for {
			if cmd.ProcessState != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	return ch
}
