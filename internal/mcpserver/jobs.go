// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// 🎯T600: no jevons_* call is long-running.
//
// Work that takes time returns a handle immediately and is queried. The
// caller is an agent whose turn is being measured: a tool that blocks for
// minutes makes a working agent indistinguishable from a wedged one, and
// on 2026-08-31 that is exactly what happened — the cockpit's 90-second
// stuck-busy timer is shorter than the 10-minute deadline most tools run
// under, so a healthy ambient cycle was entitled to roughly seven stuck
// verdicts before it was even allowed to finish.
//
// The deadlines of 🎯T254.5.1 stay, demoted to what they should always
// have been: a backstop for defects, not the mechanism that makes a call
// survivable.

// JobState is where a background job has got to.
type JobState string

const (
	JobRunning JobState = "running"
	JobDone    JobState = "done"
	JobFailed  JobState = "failed"
	// JobLost is a handle whose job did not survive a daemon restart. It
	// is a distinct answer on purpose: "I do not know what happened to
	// your work" is information, and a handle that silently resolved to
	// nothing would be worse than the blocking call this replaces.
	JobLost JobState = "lost"
)

// jobPollMax bounds the optional long-poll. A few seconds is a courtesy
// so a caller need not spin; anything longer would reintroduce the very
// blocking this exists to remove.
const jobPollMax = 5 * time.Second

// Job is one unit of background work and its outcome.
type Job struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Caller   string    `json:"caller,omitempty"`
	State    JobState  `json:"state"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitempty"`
	Result   string    `json:"result,omitempty"`
	Err      string    `json:"err,omitempty"`

	done   chan struct{}
	cancel context.CancelFunc
}

// Elapsed is how long the job has been running, or ran for.
func (j Job) Elapsed() time.Duration {
	if !j.Finished.IsZero() {
		return j.Finished.Sub(j.Started)
	}
	return time.Since(j.Started)
}

type jobRegistry struct {
	mu   sync.Mutex
	byID map[string]*Job
	dir  string
	seq  int64
}

func newJobRegistry(dir string) *jobRegistry {
	return &jobRegistry{byID: map[string]*Job{}, dir: dir}
}

func (r *jobRegistry) nextID(kind string) string {
	r.seq++
	return fmt.Sprintf("%s-%s-%d", strings.TrimPrefix(kind, "jevons_"),
		time.Now().UTC().Format("20060102T150405Z"), r.seq)
}

// start launches fn in the background and returns its handle immediately.
// fn is given a context that the caller can cancel through the handle, so
// a job is not merely un-awaited but genuinely stoppable.
func (r *jobRegistry) start(kind, caller string, fn func(context.Context) (string, error)) *Job {
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	j := &Job{
		ID:      r.nextID(kind),
		Kind:    kind,
		Caller:  caller,
		State:   JobRunning,
		Started: time.Now(),
		done:    make(chan struct{}),
		cancel:  cancel,
	}
	r.byID[j.ID] = j
	r.mu.Unlock()
	r.persist(j)

	go func() {
		defer close(j.done)
		out, err := fn(ctx)
		r.mu.Lock()
		j.Finished = time.Now()
		if err != nil {
			j.State, j.Err = JobFailed, err.Error()
		} else {
			j.State, j.Result = JobDone, out
		}
		r.mu.Unlock()
		r.persist(j)
		slog.Info("job finished", "id", j.ID, "kind", j.Kind,
			"state", j.State, "elapsed", j.Elapsed().Round(time.Second), "err", j.Err)
	}()
	return j
}

// get returns a job by id. A handle the registry has never heard of is
// reported lost rather than missing when a record for it survives on
// disk from a previous boot.
func (r *jobRegistry) get(id string) (*Job, bool) {
	r.mu.Lock()
	j, ok := r.byID[id]
	r.mu.Unlock()
	if ok {
		return j, true
	}
	if rec, ok := r.loadRecord(id); ok {
		if rec.State == JobRunning {
			// It was running when the daemon went down, so it did not
			// finish. Say so rather than leave the caller waiting.
			rec.State = JobLost
			rec.Err = "the daemon restarted while this job was running; its result was not recorded"
		}
		return rec, true
	}
	return nil, false
}

// await waits up to d for the job to finish. It is a courtesy, not the
// mechanism: d is clamped to jobPollMax.
func (r *jobRegistry) await(j *Job, d time.Duration) {
	if j == nil || j.done == nil || d <= 0 {
		return
	}
	if d > jobPollMax {
		d = jobPollMax
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-j.done:
	case <-t.C:
	}
}

func (r *jobRegistry) list() []*Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Job, 0, len(r.byID))
	for _, j := range r.byID {
		out = append(out, j)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Started.After(out[b].Started) })
	return out
}

func (r *jobRegistry) cancel(id string) bool {
	r.mu.Lock()
	j, ok := r.byID[id]
	r.mu.Unlock()
	if !ok || j.cancel == nil {
		return false
	}
	j.cancel()
	return true
}

func (r *jobRegistry) path(id string) string {
	if r.dir == "" {
		return ""
	}
	return filepath.Join(r.dir, id+".json")
}

// persist writes the record so a handle outlives the process. Best
// effort: losing the file downgrades a later query to "lost", which is
// still an honest answer.
func (r *jobRegistry) persist(j *Job) {
	p := r.path(j.ID)
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	r.mu.Lock()
	body, err := json.Marshal(j)
	r.mu.Unlock()
	if err != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, body, 0o644) == nil {
		_ = os.Rename(tmp, p)
	}
}

func (r *jobRegistry) loadRecord(id string) (*Job, bool) {
	p := r.path(id)
	if p == "" {
		return nil, false
	}
	body, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	var j Job
	if json.Unmarshal(body, &j) != nil {
		return nil, false
	}
	return &j, true
}

// FormatJobHandle is what a dispatching tool returns: the handle, and how
// to ask about it. Naming the query tool in the reply matters — the
// caller is a model, and a bare id invites it to invent a way to wait.
func FormatJobHandle(j *Job) string {
	return fmt.Sprintf("%s started — handle %s\n"+
		"Not waiting for it. Ask jevons_job(id=%q) for status and result;\n"+
		"add wait_seconds (≤%d) to block briefly, or jevons_job(id=%q, cancel=true) to stop it.",
		j.Kind, j.ID, j.ID, int(jobPollMax/time.Second), j.ID)
}

// FormatJob renders a job for the query tool.
func FormatJob(j *Job) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s (%s), elapsed %s\n", j.Kind, j.ID, j.State, j.Elapsed().Round(time.Second))
	if j.Caller != "" {
		fmt.Fprintf(&b, "  caller: %s\n", j.Caller)
	}
	switch j.State {
	case JobRunning:
		b.WriteString("  still running — ask again; do not block on it\n")
	case JobFailed, JobLost:
		fmt.Fprintf(&b, "  error: %s\n", j.Err)
	}
	if strings.TrimSpace(j.Result) != "" {
		b.WriteString("\n")
		b.WriteString(j.Result)
		if !strings.HasSuffix(j.Result, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// jobsRegistry returns the server's job registry, creating it on first
// use. Records live beside the rest of the daemon's state so a handle
// survives a bounce.
func (s *Server) jobsRegistry() *jobRegistry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jobs == nil {
		dir := ""
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, ".jevons", "jobs")
		}
		s.jobs = newJobRegistry(dir)
	}
	return s.jobs
}

// dispatchJob is what a formerly-blocking handler calls: start the work,
// hand back the handle, return now (🎯T600).
//
// wait_seconds on the call opts into a brief wait, capped at jobPollMax —
// the "at most a long poll of very limited duration" the ruling allows.
// If the work finishes inside it the caller gets the result directly and
// never has to ask; if it does not, the handle is returned as usual. The
// cap is what keeps this from sliding back into a blocking call.
func (s *Server) dispatchJob(kind, caller string, fn func(context.Context) (string, error)) string {
	return s.dispatchJobWaiting(kind, caller, 0, fn)
}

func (s *Server) dispatchJobWaiting(kind, caller string, wait time.Duration,
	fn func(context.Context) (string, error)) string {
	reg := s.jobsRegistry()
	j := reg.start(kind, caller, fn)
	if wait > 0 {
		reg.await(j, wait)
		reg.mu.Lock()
		state, result, jerr := j.State, j.Result, j.Err
		reg.mu.Unlock()
		switch state {
		case JobDone:
			return result
		case JobFailed:
			return fmt.Sprintf("%s failed after %s: %s", kind, j.Elapsed().Round(time.Second), jerr)
		}
	}
	return FormatJobHandle(j)
}

// jobWaitArg reads an optional wait_seconds from a tool call.
func jobWaitArg(req mcp.CallToolRequest) time.Duration {
	if v, ok := req.GetArguments()["wait_seconds"].(float64); ok && v > 0 {
		return time.Duration(v * float64(time.Second))
	}
	return 0
}

// withJobWait adds the shared wait_seconds option to a dispatching tool.
func withJobWait() mcp.ToolOption {
	return mcp.WithNumber("wait_seconds",
		mcp.Description("Block briefly for the result instead of returning a handle (capped at 5s)."))
}

func (s *Server) registerJobTools() {
	s.addTool(
		mcp.NewTool("jevons_job",
			mcp.WithDescription(
				"Status and result of background work started by another jevons_* tool (🎯T600). "+
					"Tools that take time return a handle instead of blocking; this reads it. "+
					"States: running | done | failed | lost (the daemon restarted mid-job). "+
					"Omit id to list recent jobs."),
			mcp.WithString("id", mcp.Description("Job handle returned by the dispatching tool")),
			mcp.WithNumber("wait_seconds",
				mcp.Description("Block briefly for the job to finish (capped at 5s). Omit to return immediately.")),
			mcp.WithBoolean("cancel", mcp.Description("Cancel the job instead of reading it")),
		),
		s.handleJob,
	)
}

func (s *Server) handleJob(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	reg := s.jobsRegistry()
	id := strings.TrimSpace(str(args["id"]))
	if id == "" {
		jobs := reg.list()
		if len(jobs) == 0 {
			return mcp.NewToolResultText("no background jobs"), nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d background job(s), newest first:\n", len(jobs))
		for _, j := range jobs {
			fmt.Fprintf(&b, "  %-10s %-9s %s (%s)\n", j.State, j.Elapsed().Round(time.Second), j.ID, j.Kind)
		}
		return mcp.NewToolResultText(b.String()), nil
	}
	j, ok := reg.get(id)
	if !ok {
		return mcp.NewToolResultError(fmt.Sprintf("no job %q — it never existed on this daemon", id)), nil
	}
	if b, _ := args["cancel"].(bool); b {
		if reg.cancel(id) {
			return mcp.NewToolResultText("cancel requested for " + id), nil
		}
		return mcp.NewToolResultText(id + " is not cancellable (already finished, or not from this boot)"), nil
	}
	if w, ok := args["wait_seconds"].(float64); ok && w > 0 {
		reg.await(j, time.Duration(w*float64(time.Second)))
	}
	return mcp.NewToolResultText(FormatJob(j)), nil
}
