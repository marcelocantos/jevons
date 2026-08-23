// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/doit"
	"github.com/marcelocantos/jevons/internal/workers"
)

const (
	// maxWorkerDepth is the maximum nesting depth for jwork calls.
	// Workers at this depth cannot spawn further workers.
	maxWorkerDepth = 3

	// headingThrottleInterval is the minimum time between progress
	// heartbeats for headings at the same depth.
	headingThrottleInterval = 2 * time.Second
)

// headingPattern matches markdown headings (# through ######).
var headingPattern = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)

// registerJwork adds the jwork MCP tool to the server.
func (s *Server) registerJwork() {
	s.mcpSrv.AddTool(
		mcp.NewTool("jwork",
			mcp.WithDescription(
				"Dispatch a task to an on-demand Grok Build worker. "+
					"The worker is a fresh subprocess that runs the task to completion and returns the result. "+
					"Task description must be self-contained — no implicit context is injected. "+
					"Policy decisions (doit) are returned in structuredContent.metadata (🎯T8.3)."),
			mcp.WithString("text", mcp.Required(), mcp.Description("Task description. Must be self-contained — the worker has no prior context.")),
			mcp.WithString("cwd", mcp.Description("Working directory for the worker (defaults to the coordinator's default)")),
			mcp.WithString("model", mcp.Description("Model override (e.g. 'grok-4'; empty = provider default)")),
			mcp.WithString("provider", mcp.Description("Agent backend override (claudia provider id: grok, claude, …). Empty = daemon default. 🎯T148.")),
			mcp.WithNumber("depth", mcp.Description("Current call depth (0 = top-level). Workers increment this when calling jwork themselves. Do not set manually.")),
		),
		s.handleJwork,
	)
}

func (s *Server) handleJwork(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if blocked := s.checkSpawnAllowed(); blocked != nil {
		return blocked, nil
	}

	args := req.GetArguments()
	text, _ := args["text"].(string)
	cwd, _ := args["cwd"].(string)
	model, _ := args["model"].(string)
	providerArg, _ := args["provider"].(string)
	depthF, _ := args["depth"].(float64)
	depth := int(depthF)
	provider := cli.SelectAgentProvider(providerArg, "", s.resolvedDefaultProvider())

	if text == "" {
		return mcp.NewToolResultError("missing required parameter: text"), nil
	}
	if depth >= maxWorkerDepth {
		return mcp.NewToolResultError(fmt.Sprintf(
			"maximum worker depth (%d) reached — cannot spawn further workers. "+
				"Complete the task directly instead of delegating.", maxWorkerDepth)), nil
	}
	if cwd == "" {
		cwd = s.workerWD
	}
	// Expand ~ in cwd.
	if strings.HasPrefix(cwd, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			cwd = home + cwd[1:]
		}
	}

	workerID := uuid.New().String()[:8]
	nextDepth := depth + 1

	// 🎯T8.3: gate spawn through doit Evaluate/Execute + audit.
	policy := doit.GateSpawn(ctx, s.doitEng, text, cwd)
	if policy.Decision == "deny" {
		slog.Warn("jwork: policy denied spawn",
			"worker", workerID,
			"decision", policy.Decision,
			"level", policy.Level,
			"reason", policy.Reason,
		)
		// 🎯T335: surface policy deny to standing security auditor (not silent).
		s.notifySecurityPolicyDeny(workerID, policy.Reason)
		if s.workers != nil {
			_ = s.workers.Start(workers.StartArgs{
				ID: workerID, Task: text, Model: model, Cwd: cwd,
			})
			_ = s.workers.Finish(workers.FinishArgs{
				ID: workerID, Status: workers.StatusDenied,
				Outcome: "policy denied: " + policy.Reason,
				Policy: &workers.PolicyArgs{
					Decision: policy.Decision,
					Level:    policy.Level,
					Reason:   policy.Reason,
					RuleID:   policy.RuleID,
					AuditSeq: policy.AuditSeq,
				},
			})
		}
		return mcp.NewToolResultStructured(map[string]any{
			"result":    "",
			"worker_id": workerID,
			"status":    workers.StatusDenied,
			"policy": map[string]any{
				"decision":  policy.Decision,
				"level":     policy.Level,
				"reason":    policy.Reason,
				"rule_id":   policy.RuleID,
				"audit_seq": policy.AuditSeq,
			},
		}, fmt.Sprintf("policy denied jwork spawn: %s", policy.Reason)), nil
	}

	slog.Info("jwork: dispatching worker",
		"worker", workerID,
		"depth", depth,
		"cwd", cwd,
		"model", model,
		"provider", provider,
		"policy", policy.Decision,
		"policy_level", policy.Level,
	)

	if s.workers != nil {
		if err := s.workers.Start(workers.StartArgs{
			ID: workerID, Task: text, Model: model, Cwd: cwd,
		}); err != nil {
			slog.Warn("jwork: worker track start failed", "err", err)
		} else {
			_ = s.workers.RecordPolicy(workerID, workers.PolicyArgs{
				Decision: policy.Decision,
				Level:    policy.Level,
				Reason:   policy.Reason,
				RuleID:   policy.RuleID,
				AuditSeq: policy.AuditSeq,
			})
		}
	}

	// Build the prompt. At higher depths, inject delegation guidance.
	prompt := text
	if nextDepth >= maxWorkerDepth {
		prompt = delegationGuidance + "\n\n" + text
	}

	task := claudia.NewTask(claudia.TaskConfig{
		ID:       workerID,
		Name:     fmt.Sprintf("jwork-d%d-%s", depth, workerID),
		Provider: provider,
		WorkDir:  cwd,
		Model:    model,
	})

	events, err := task.Run(ctx, prompt)
	if err != nil {
		// 🎯T283: name the class so the caller can tell an outage from a bad task.
		dispatchClass, dispatchMsg := agenterr.ClassifyAndFormat(err)
		slog.Error("jwork: dispatch failed",
			"worker", workerID,
			"err", err,
			"failure_class", dispatchClass.String(),
			"transient", dispatchClass.IsTransient(),
		)
		if s.workers != nil {
			_ = s.workers.Finish(workers.FinishArgs{
				ID: workerID, Status: workers.StatusFailed,
				Outcome: err.Error(),
				Policy: &workers.PolicyArgs{
					Decision: policy.Decision,
					Level:    policy.Level,
					Reason:   policy.Reason,
					RuleID:   policy.RuleID,
					AuditSeq: policy.AuditSeq,
				},
			})
		}
		if dispatchClass.IsFailure() {
			s.logProviderFailure("jwork", workerID, dispatchClass, err.Error())
			return mcp.NewToolResultError("worker dispatch failed: " + dispatchMsg), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("worker dispatch failed: %v", err)), nil
	}

	// Collect result while extracting progress heartbeats and token totals.
	var textParts []string
	var inputTok, outputTok int64
	var costUSD float64
	var errClass agenterr.Class
	var errRaw string
	hb := &heartbeatTracker{}

	for ev := range events {
		switch ev.Type {
		case claudia.TaskEventText:
			textParts = append(textParts, ev.Content)
			if s.workers != nil {
				for _, line := range strings.Split(ev.Content, "\n") {
					if strings.TrimSpace(line) != "" {
						_ = s.workers.Progress(workerID, line)
					}
				}
			}
			// Check each line for markdown headings.
			for _, line := range strings.Split(ev.Content, "\n") {
				if heading, hdepth := extractHeading(line); heading != "" {
					if hb.shouldEmit(hdepth) {
						slog.Info("jwork: progress",
							"worker", workerID,
							"depth", depth,
							"heading_depth", hdepth,
							"heading", heading,
						)
						s.broadcastWorkerProgress(workerID, depth, heading)
					}
				}
			}
		case claudia.TaskEventResult:
			inputTok += int64(ev.Usage.InputTokens)
			outputTok += int64(ev.Usage.OutputTokens)
			costUSD += ev.CostUSD
			if ev.Content != "" {
				textParts = append(textParts, ev.Content)
			}
		case claudia.TaskEventError:
			// 🎯T283: keep the strongest class seen. Previously these were
			// logged and dropped, so a backend outage surfaced to the caller
			// as "Worker finished (no output)" — a product defect, apparently.
			if c := agenterr.ClassifyText(ev.ErrorMsg); c.IsFailure() {
				errClass, errRaw = c, ev.ErrorMsg
			}
			slog.Warn("jwork: worker error",
				"worker", workerID,
				"error", ev.ErrorMsg,
				"failure_class", agenterr.ClassifyText(ev.ErrorMsg).String(),
			)
			if s.workers != nil {
				_ = s.workers.Progress(workerID, "error: "+ev.ErrorMsg)
			}
		}
	}

	result := task.LastResult()
	if result == "" {
		result = strings.Join(textParts, "")
	}

	failClass, failMsg := jworkFailure(errClass, errRaw, result)
	if failClass.IsFailure() {
		s.logProviderFailure("jwork", workerID, failClass, failMsg)
		if s.workers != nil {
			_ = s.workers.Finish(workers.FinishArgs{
				ID: workerID, Status: workers.StatusFailed,
				Outcome:      truncate(failMsg, 500),
				InputTokens:  inputTok,
				OutputTokens: outputTok,
				CostUSD:      costUSD,
			})
		}
		return mcp.NewToolResultStructured(map[string]any{
			"result":        failMsg,
			"worker_id":     workerID,
			"status":        workers.StatusFailed,
			"failure_class": failClass.String(),
			"transient":     failClass.IsTransient(),
			"input_tokens":  inputTok,
			"output_tokens": outputTok,
			"cost_usd":      costUSD,
		}, failMsg), nil
	}

	if result == "" {
		result = "Worker finished (no output)."
	}

	s.ObserveProviderOK()

	slog.Info("jwork: worker complete",
		"worker", workerID,
		"depth", depth,
		"result_len", len(result),
	)

	if s.workers != nil {
		_ = s.workers.Finish(workers.FinishArgs{
			ID: workerID, Status: workers.StatusCompleted,
			Outcome:      truncate(result, 500),
			InputTokens:  inputTok,
			OutputTokens: outputTok,
			CostUSD:      costUSD,
			Policy: &workers.PolicyArgs{
				Decision: policy.Decision,
				Level:    policy.Level,
				Reason:   policy.Reason,
				RuleID:   policy.RuleID,
				AuditSeq: policy.AuditSeq,
			},
		})
	}

	// Text remains the primary human/agent payload; structuredContent carries
	// policy + worker id for machine consumers (🎯T8.3 metadata).
	truncated := truncate(result, 4000)
	// 🎯T283: a run that hit provider errors but still produced output carries
	// the class so the caller can tell a clean run from a recovered one.
	return mcp.NewToolResultStructured(map[string]any{
		"result":        truncated,
		"worker_id":     workerID,
		"status":        workers.StatusCompleted,
		"failure_class": errClass.String(),
		"transient":     errClass.IsTransient(),
		"input_tokens":  inputTok,
		"output_tokens": outputTok,
		"cost_usd":      costUSD,
		"policy": map[string]any{
			"decision":  policy.Decision,
			"level":     policy.Level,
			"reason":    policy.Reason,
			"rule_id":   policy.RuleID,
			"audit_seq": policy.AuditSeq,
		},
	}, truncated), nil
}

// extractHeading parses a markdown heading line, returning the text
// and heading depth (1-6). Returns ("", 0) if not a heading.
func extractHeading(line string) (string, int) {
	m := headingPattern.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return "", 0
	}
	return m[2], len(m[1])
}

// heartbeatTracker throttles progress heartbeats by heading depth.
type heartbeatTracker struct {
	mu        sync.Mutex
	lastByLvl map[int]time.Time
}

func (h *heartbeatTracker) shouldEmit(level int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.lastByLvl == nil {
		h.lastByLvl = make(map[int]time.Time)
	}

	now := time.Now()
	if last, ok := h.lastByLvl[level]; ok && now.Sub(last) < headingThrottleInterval {
		return false
	}
	h.lastByLvl[level] = now
	return true
}

// broadcastWorkerProgress sends a progress heartbeat to the web UI
// and notifies the Jevon overseer.
func (s *Server) broadcastWorkerProgress(workerID string, depth int, heading string) {
	s.mu.Lock()
	fn := s.notifyJevon
	s.mu.Unlock()

	if fn != nil {
		fn(fmt.Sprintf("[jwork %s d%d] %s", workerID, depth, heading))
	}
}

const delegationGuidance = `IMPORTANT: You are at the maximum delegation depth. ` +
	`You MUST complete this task directly — do not attempt to delegate to sub-workers ` +
	`or call the jwork tool. If the task is too large, do what you can and clearly ` +
	`state what remains unfinished.`
