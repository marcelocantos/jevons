// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/secauditor"
	"github.com/marcelocantos/jevons/internal/writconf"
)

// SetSecurityAuditor attaches the standing security interest (🎯T335).
func (s *Server) SetSecurityAuditor(a *secauditor.Interest) {
	if s == nil {
		return
	}
	s.secAuditor = a
}

// SetWritExecutor attaches confined exec + writ availability (🎯T335).
func (s *Server) SetWritExecutor(ex writconf.Executor, bin string, available bool) {
	if s == nil {
		return
	}
	s.writExec = ex
	s.writBin = bin
	s.writAvailable = available
}

// registerWritSecurityTools adds jevons_writ_exec + status (🎯T335).
func (s *Server) registerWritSecurityTools() {
	s.addTool(
		mcp.NewTool("jevons_writ_exec",
			mcp.WithDescription(
				"Run a high-risk command under writ declared-intent confinement "+
					"(seatbelt + egress proxy). Compiles a fleet writ manifest; "+
					"undeclared net/fs is denied and surfaces to the security auditor (🎯T335). "+
					"Prefer this over unconstrained shell for opaque child exec."),
			mcp.WithString("command", mcp.Required(), mcp.Description("Command to run (argv0)")),
			mcp.WithArray("args", mcp.Description("Additional argv after command"), mcp.WithStringItems()),
			mcp.WithString("cwd", mcp.Description("Working directory (default: worker WD)")),
			mcp.WithString("agent", mcp.Description("Fleet agent name for auditor correlation")),
			mcp.WithBoolean("include_fs", mcp.Description("Include workdir FS scopes (needs FUSE under real writ)")),
			mcp.WithBoolean("pure", mcp.Description("Hermetic pure allowlist only (no seatbelt child)")),
		),
		s.handleWritExec,
	)
	s.addTool(
		mcp.NewTool("jevons_security_status",
			mcp.WithDescription("Standing security auditor + writ confinement status (🎯T335)."),
		),
		s.handleSecurityStatus,
	)
}

func (s *Server) handleSecurityStatus(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	st := map[string]any{
		"auditor":        secauditor.DefaultName,
		"ready":          s.secAuditor != nil,
		"confinement":    "writ",
		"writ_available": s.writAvailable,
		"writ_bin":       s.writBin,
	}
	if s.secAuditor != nil {
		st["recent_alerts"] = len(s.secAuditor.Recent())
	}
	return mcp.NewToolResultStructured(st, fmt.Sprintf(
		"security auditor ready=%v writ=%v", s.secAuditor != nil, s.writAvailable)), nil
}

func (s *Server) handleWritExec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	cmd, _ := args["command"].(string)
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return mcp.NewToolResultError("missing required parameter: command"), nil
	}
	var rest []string
	if raw, ok := args["args"].([]any); ok {
		for _, a := range raw {
			if ss, ok := a.(string); ok {
				rest = append(rest, ss)
			}
		}
	}
	cwd, _ := args["cwd"].(string)
	if cwd == "" {
		cwd = s.workerWD
	}
	agent, _ := args["agent"].(string)
	includeFS, _ := args["include_fs"].(bool)
	pure, _ := args["pure"].(bool)

	m, err := writconf.FleetManifest(writconf.FleetManifestArgs{
		WorkDir:   cwd,
		IncludeFS: includeFS,
	})
	if err != nil {
		return mcp.NewToolResultError("manifest: " + err.Error()), nil
	}

	ex := s.writExec
	if pure || ex == nil {
		ex = writconf.PureExecutor{}
	}
	argv := append([]string{cmd}, rest...)
	res, err := ex.Exec(ctx, &writconf.ExecArgs{
		Manifest: m,
		Argv:     argv,
		WorkDir:  cwd,
		Agent:    agent,
		Timeout:  45 * time.Second,
	})
	if res != nil && res.Denied && res.Event != nil {
		s.observeSecurity(*res.Event)
	}
	if !pure && !s.writAvailable && res != nil && !res.Denied {
		s.observeSecurity(writconf.ClassifyBypass(agent, "jevons_writ_exec without writ binary"))
	}

	payload := map[string]any{
		"denied":   res != nil && res.Denied,
		"manifest": m,
	}
	if res != nil {
		payload["exit_code"] = res.ExitCode
		payload["stdout"] = res.Stdout
		payload["stderr"] = res.Stderr
		payload["used_writ"] = res.UsedWrit
		if res.Event != nil {
			payload["event"] = res.Event
		}
	}
	if err != nil {
		payload["error"] = err.Error()
		return mcp.NewToolResultStructured(payload, "writ exec failed: "+err.Error()), nil
	}
	if res != nil && res.Denied {
		msg := "confinement denied"
		if res.Event != nil {
			msg = res.Event.Message
		}
		return mcp.NewToolResultStructured(payload, msg), nil
	}
	out := ""
	if res != nil {
		out = res.Stdout
	}
	return mcp.NewToolResultStructured(payload, out), nil
}

// observeSecurity routes a confinement event to the auditor + eventlog.
func (s *Server) observeSecurity(ev writconf.Event) {
	if s == nil {
		return
	}
	if s.secAuditor != nil {
		s.secAuditor.Observe(ev)
		return
	}
	// Fallback dual-write when auditor not wired.
	s.logLifecycle(secauditor.Component, string(ev.Kind), "alert_unwired", map[string]any{
		"agent": ev.Agent,
		"host":  ev.Host,
		"path":  ev.Path,
	})
}

// notifySecurityPolicyDeny surfaces jwork/doit denies to the auditor (🎯T335).
func (s *Server) notifySecurityPolicyDeny(agent, reason string) {
	s.observeSecurity(writconf.Event{
		Kind:    writconf.KindPolicyDeny,
		Agent:   agent,
		Message: reason,
	})
}

// securityControlPlane adapts budget halt + registry kill for the auditor.
type securityControlPlane struct {
	haltSpawn func(reason string) error
	kill      func(name, reason string) error
	file      func(name, acceptance string) error
}

func (c securityControlPlane) HaltSpawn(reason string) error {
	if c.haltSpawn == nil {
		return nil
	}
	return c.haltSpawn(reason)
}
func (c securityControlPlane) KillWorker(name, reason string) error {
	if c.kill == nil {
		return nil
	}
	return c.kill(name, reason)
}
func (c securityControlPlane) FileTarget(name, acceptance string) error {
	if c.file == nil {
		slog.Info("security auditor file_target residual", "name", name, "acceptance", acceptance)
		return nil
	}
	return c.file(name, acceptance)
}

// securityAlertDeliverer pushes judgments onto the overseer notify path.
type securityAlertDeliverer struct {
	send func(text string) error
}

func (d securityAlertDeliverer) DeliverAlert(text string) error {
	if d.send == nil {
		return nil
	}
	return d.send(text)
}

// NewSecurityInterest builds an auditor wired to MCP eventlog + notify.
// Call from main after notify and registry are available.
func NewSecurityInterest(s *Server, notify func(string) error) *secauditor.Interest {
	a := secauditor.New()
	a.Deliverer = securityAlertDeliverer{send: notify}
	a.Control = securityControlPlane{
		haltSpawn: func(reason string) error {
			if s == nil || s.spawnGuard == nil {
				slog.Warn("security halt_spawn requested", "reason", reason)
				return nil
			}
			// spawnGuard only allows/denies; halt is cost enforcer territory.
			// Log + surface — residual hard halt may use cost killswitch.
			slog.Warn("security auditor halt_spawn", "reason", reason)
			return nil
		},
		kill: func(name, reason string) error {
			if s == nil || s.registry == nil {
				return nil
			}
			// Soft path: log; hard kill via Stop when process exists.
			if p := s.registry.Get(name); p != nil {
				p.Stop()
				slog.Warn("security auditor killed worker", "agent", name, "reason", reason)
			}
			return nil
		},
	}
	a.Log = func(level, component, decision, msg string, fields map[string]any) {
		out := map[string]any{}
		for k, v := range fields {
			out[k] = v
		}
		if level != "" {
			out["level"] = level
		}
		if msg != "" {
			out["msg"] = msg
		}
		if s != nil {
			s.LogEvent(component, decision, out)
			return
		}
		slog.Warn(msg, "component", component, "decision", decision)
	}
	return a
}
