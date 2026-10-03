package missionmeter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestScanProxyPopulationTargetsAndWindow(t *testing.T) {
	ev := fixture(t, "events.jsonl", strings.Join([]string{
		`{"ts":"2026-10-03T01:00:00Z","component":"agent_lifecycle","decision":"start","fields":{"outcome":"ok","name":"jv-a","target_id":"T1"}}`,
		`{"ts":"2026-10-03T01:01:00Z","component":"agent_lifecycle","decision":"start","fields":{"outcome":"ok","name":"jv-a","target_id":"T1"}}`,
		`{"ts":"2026-10-03T01:01:00Z","component":"agent_lifecycle","decision":"start","fields":{"outcome":"error","name":"jv-a"}}`,
		`{"ts":"2026-10-03T01:00:00Z","component":"agent_lifecycle","decision":"start","fields":{"outcome":"ok","name":"jevons-po","target_id":"T1"}}`,
		`{"ts":"2026-10-04T00:00:00Z","component":"agent_lifecycle","decision":"start","fields":{"outcome":"ok","name":"jv-a"}}`,
	}, "\n")+"\n")
	spool := fixture(t, "spool.log", strings.Join([]string{
		`{"ts":"2026-10-03T02:00:00Z","seat":"jv-a","type":"turn_end","snapshot":{"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"world"}]}}`,
		`{"ts":"2026-10-03T02:01:00Z","seat":"jv-a","type":"turn_end","snapshot":{"messages":[{"role":"user","content":"hello"}]}}`,
		`{"ts":"2026-10-03T02:00:00Z","seat":"jv-b","type":"turn_end","snapshot":{"messages":[{"role":"user","content":"x"}]}}`,
		`{"ts":"2026-10-03T02:00:00Z","seat":"jevons-po","type":"turn_end","snapshot":{"messages":[{"role":"user","content":"p"}]}}`,
		`{"ts":"2026-10-03T02:00:00Z","seat":"jevons","type":"turn_end","snapshot":{"messages":[]}}`,
	}, "\n")+"\n")
	to, _ := time.Parse(time.RFC3339, "2026-10-04T00:00:00Z")
	r, err := Scan([]string{spool}, []string{ev}, Window{To: to})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Seats) != 4 || r.WorkerSum.Distribution.Count != 2 || len(r.Targets) != 1 {
		t.Fatalf("unexpected population: %+v", r)
	}
	a := r.Seats[0]
	if a.Name != "jevons" {
		t.Fatalf("sorting: %+v", r.Seats)
	}
	var worker Seat
	for _, s := range r.Seats {
		if s.Name == "jv-a" {
			worker = s
		}
	}
	first := int64(len(`{"role":"user","content":"hello"}`) + len(`{"role":"assistant","content":"world"}`))
	if worker.Starts != 2 || worker.Turns != 2 || worker.MaxMessageBytes != first || worker.MessageBytes != first+int64(len(`{"role":"user","content":"hello"}`)) || worker.TargetID != "T1" {
		t.Fatalf("worker proxy %+v", worker)
	}
	if r.Targets[0].Starts != 3 || r.Targets[0].Seats != 2 || r.WorkerStarts.Ranking[0].Seat != "jv-a" || r.WorkerStarts.Ranking[0].Rank != 1 {
		t.Fatalf("aggregation %+v", r)
	}
}
func TestMalformedVisible(t *testing.T) {
	for _, v := range []string{`{"type":"turn_end","seat":"jv-x","ts":"invalid"}` + "\n", `{broken}` + "\n", `{}`} {
		p := fixture(t, "bad.log", v)
		_, err := Scan([]string{p}, nil, Window{})
		if err == nil || !strings.Contains(err.Error(), "bad.log:1:") {
			t.Fatalf("missing path and line: %v", err)
		}
	}
}
func TestPercentilesAndZeroVariance(t *testing.T) {
	m := metric([]Seat{{Name: "a", Tier: "worker", Starts: 1}, {Name: "b", Tier: "worker", Starts: 1}, {Name: "jevons-po", Tier: "po", Starts: 99}}, func(s Seat) int64 { return int64(s.Starts) })
	if m.Distribution.Count != 2 || m.Distribution.P90 != 1 || m.Distribution.Sigma != 0 || m.Ranking[0].Sigma != 0 {
		t.Fatalf("distribution %+v", m)
	}
}
