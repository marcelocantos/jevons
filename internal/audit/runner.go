// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Assignment is one bounded audit pass handed to the auditor.
type Assignment struct {
	Manifest Manifest
	Prompt   string
	Provider string
	Model    string
	Workdir  string
	Timeout  time.Duration
	// MaxOutputBytes caps bytes read back from the auditor.
	MaxOutputBytes int
	// Command is the resolved invocation (model already substituted).
	Command []string
	Reason  string
}

// RunOutput is the auditor's raw answer plus the tier that produced it.
type RunOutput struct {
	Raw      []byte
	Provider string
	Model    string
}

// Runner dispatches an assignment to an auditor. Production is the
// advanced-model CLI (ExecRunner); tests inject a fixture runner, which is
// what keeps the cycle hermetically testable without a live model.
type Runner interface {
	RunAudit(ctx context.Context, a Assignment) (RunOutput, error)
}

// RunnerFunc adapts a function to Runner.
type RunnerFunc func(ctx context.Context, a Assignment) (RunOutput, error)

// RunAudit implements Runner.
func (f RunnerFunc) RunAudit(ctx context.Context, a Assignment) (RunOutput, error) {
	return f(ctx, a)
}

// ExecRunner invokes the configured advanced-model CLI, feeding the prompt
// on stdin and reading the report from stdout. The auditor runs in the
// repository with its own file tools: the prompt carries the bounded file
// list, not the file contents, so a full-scan manifest stays affordable.
type ExecRunner struct {
	// Env optionally overrides the child environment (nil = inherit).
	Env []string
}

// RunAudit implements Runner over the configured command.
func (r ExecRunner) RunAudit(ctx context.Context, a Assignment) (RunOutput, error) {
	out := RunOutput{Provider: a.Provider, Model: a.Model}
	if len(a.Command) == 0 {
		return out, fmt.Errorf("audit runner: no command configured")
	}
	timeout := a.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, a.Command[0], a.Command[1:]...)
	if w := strings.TrimSpace(a.Workdir); w != "" {
		cmd.Dir = w
	}
	if r.Env != nil {
		cmd.Env = r.Env
	}
	cmd.Stdin = strings.NewReader(a.Prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	max := a.MaxOutputBytes
	if max <= 0 {
		max = DefaultMaxOutputBytes
	}
	raw := stdout.Bytes()
	if len(raw) > max {
		raw = raw[:max]
	}
	out.Raw = raw
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return out, fmt.Errorf("audit runner: timed out after %s", timeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 512 {
			msg = msg[:512]
		}
		return out, fmt.Errorf("audit runner: %s: %w (stderr: %s)", a.Command[0], err, msg)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return out, fmt.Errorf("audit runner: empty output from %s", a.Command[0])
	}
	return out, nil
}

// DefaultSystemPrompt is the standing auditor brief. It states the job,
// the bounds, and the exact answer shape. The severity ladder and the
// suggested-target shape are spelled out because the residue ledger keys
// off them.
const DefaultSystemPrompt = `You are the Jevons periodic full-scan auditor (🎯T357).

Audit the files listed in the SCOPE MANIFEST below across three surfaces:
  - code:    product source — correctness, resource/lifecycle bugs, error
             handling, concurrency hazards, silent-failure paths, dead or
             duplicated logic, missing oracles on load-bearing behaviour.
  - skills:  skill definitions — stale instructions, broken references to
             files or commands, contradictions with project doctrine.
  - prompts: agent prompts, persona, and standing briefs — internal
             contradictions, rules that no longer match the product,
             ambiguity that would cause an agent to act wrongly.

Rules:
  - Read files from the manifest. Do NOT modify, rewrite, or commit anything:
    this is a read-only audit pass.
  - Report only ACTIONABLE findings with concrete evidence (file and, where
    it applies, line). No style opinions, no restating what the code does,
    no speculative "could be refactored" notes.
  - Prefer a small number of real findings over exhaustive noise. If a
    surface is clean, say so with zero findings for it.
  - severity is one of: info, low, medium, high, critical.
    critical = data loss, security exposure, or an owner-visible break.
    high     = a real defect that will bite under normal use.
    medium   = a defect with limited blast radius or a doctrine breach.
    low/info = worth knowing, not worth interrupting anyone.
  - suggested_target is optional; include it when the finding warrants a
    tracked convergence target. Acceptance items are assertions of a
    desired state, not tasks.

Answer with a SINGLE JSON object and nothing else:

{
  "summary": "one or two sentences on the state of the audited surfaces",
  "findings": [
    {
      "scope": "code|skills|prompts",
      "path": "relative/path/to/file",
      "line": 123,
      "severity": "critical|high|medium|low|info",
      "title": "short stable one-line statement of the defect",
      "detail": "what is wrong and why it matters",
      "evidence": "the concrete pointer that makes this checkable",
      "suggested_target": {
        "name": "desired state as an assertion",
        "acceptance": ["assertion 1", "assertion 2"]
      }
    }
  ]
}

Keep "title" stable and specific: it is the identity of the finding across
audit passes, so the same defect must produce the same title next time.`

// BuildPrompt renders the auditor brief plus the bounded scope manifest.
// Truncation is stated explicitly so the auditor never reports a surface
// clean when it was only shown part of it.
func BuildPrompt(cfg Config, m Manifest, maxFindings int) string {
	var b strings.Builder
	brief := strings.TrimSpace(cfg.SystemPrompt)
	if brief == "" {
		brief = DefaultSystemPrompt
	}
	b.WriteString(brief)
	b.WriteString("\n\nReport at most ")
	fmt.Fprintf(&b, "%d", maxFindings)
	b.WriteString(" findings, most severe first.\n")

	if w := strings.TrimSpace(m.Workdir); w != "" {
		b.WriteString("\nWorking directory: ")
		b.WriteString(w)
		b.WriteString("\n")
	}
	if missing := m.MissingScopes(); len(missing) > 0 {
		b.WriteString("\nNOTE: this is a PARTIAL scan — no files were in scope for: ")
		b.WriteString(joinScopes(missing))
		b.WriteString(". Report nothing about those surfaces.\n")
	}

	b.WriteString("\n=== SCOPE MANIFEST ===\n")
	for _, s := range m.Scopes {
		fmt.Fprintf(&b, "\n## scope: %s (%d files, %s)\n", s.Kind, len(s.Files), humanBytes(s.TotalBytes))
		if len(s.Roots) > 0 {
			fmt.Fprintf(&b, "roots: %s\n", strings.Join(s.Roots, ", "))
		}
		if s.Truncated {
			fmt.Fprintf(&b, "TRUNCATED: %d further in-scope files were omitted by the pass bounds; "+
				"do not conclude this surface is clean beyond the listed files.\n", s.Omitted)
		}
		if len(s.Files) == 0 {
			b.WriteString("(no files in scope)\n")
			continue
		}
		for _, f := range s.Files {
			b.WriteString("  ")
			b.WriteString(NormalizePath(f.Path, m.Workdir))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n=== END SCOPE MANIFEST ===\n")
	b.WriteString("\nAnswer with the JSON object only.\n")
	return b.String()
}

func joinScopes(kinds []ScopeKind) string {
	parts := make([]string, 0, len(kinds))
	for _, k := range kinds {
		parts = append(parts, string(k))
	}
	return strings.Join(parts, ", ")
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
