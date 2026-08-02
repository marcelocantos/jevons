// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package selftest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidSite(t *testing.T) {
	for _, s := range []Site{SiteLive, SiteDrill, SiteCI} {
		if !ValidSite(s) {
			t.Fatalf("want valid %q", s)
		}
	}
	if ValidSite("prod") {
		t.Fatal("prod must be invalid")
	}
}

func TestGradeFromMeasurements(t *testing.T) {
	if GradeFromMeasurements(nil) != GradeError {
		t.Fatal("empty → error")
	}
	if GradeFromMeasurements([]Measurement{{ID: "a", OK: true}}) != GradePass {
		t.Fatal("all ok → pass")
	}
	if GradeFromMeasurements([]Measurement{{ID: "a", OK: true}, {ID: "b", OK: false}}) != GradeFail {
		t.Fatal("any fail → fail")
	}
}

func TestVisibleRatioAndNearBottom(t *testing.T) {
	// Fully visible
	if r := VisibleRatio(10, 50, 0, 100); r != 1 {
		t.Fatalf("full visible ratio=%v", r)
	}
	// Half visible
	if r := VisibleRatio(75, 50, 0, 100); r < 0.49 || r > 0.51 {
		t.Fatalf("half ratio=%v", r)
	}
	// Not visible
	if r := VisibleRatio(200, 50, 0, 100); r != 0 {
		t.Fatalf("out of view ratio=%v", r)
	}
	if !NearBottom(900, 1000, 100, 5) {
		t.Fatal("near bottom")
	}
	if NearBottom(0, 1000, 100, 5) {
		t.Fatal("at top not near bottom")
	}
	if !NearBottom(0, 50, 100, 5) {
		t.Fatal("content shorter than viewport is near bottom")
	}
}

func TestGrowthWithoutCoverPolicy(t *testing.T) {
	ratio, ok := GrowthWithoutCover(800, ComposerMaxVh, 500, 120, MinLastReplyVisible)
	if !ok || ratio < MinLastReplyVisible {
		t.Fatalf("policy fail ratio=%v ok=%v", ratio, ok)
	}
	// Cap above 40vh is unsafe
	if _, ok := GrowthWithoutCover(800, 50, 500, 120, MinLastReplyVisible); ok {
		t.Fatal("50vh must fail policy")
	}
}

func TestHealthL1InProcess(t *testing.T) {
	env := &Env{
		Site: SiteCI,
		HealthGET: func(ctx context.Context) (int, map[string]any, error) {
			return 200, map[string]any{"status": "ok", "version": "test"}, nil
		},
	}
	res, err := Run(context.Background(), env, RunRequest{Pack: "health-L1", Site: SiteCI})
	if err != nil {
		t.Fatal(err)
	}
	if res.Single == nil || res.Single.Grade != GradePass {
		t.Fatalf("report=%+v", res.Single)
	}
	if res.Single.SchemaVersion != SchemaVersion {
		t.Fatalf("schema %d", res.Single.SchemaVersion)
	}
}

func TestHealthL1HTTP(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": "v"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	env := &Env{Site: SiteLive, BaseURL: srv.URL}
	res, err := Run(context.Background(), env, RunRequest{Pack: "health-L1", Site: SiteLive})
	if err != nil {
		t.Fatal(err)
	}
	if res.Single.Grade != GradePass {
		t.Fatalf("grade=%s narrative=%s", res.Single.Grade, res.Single.Narrative)
	}
}

func TestComposerGrowthL2Pack(t *testing.T) {
	res, err := Run(context.Background(), &Env{Site: SiteCI}, RunRequest{Pack: "composer-growth-L2", Site: SiteCI})
	if err != nil {
		t.Fatal(err)
	}
	if res.Single.Grade != GradePass {
		t.Fatalf("%+v", res.Single)
	}
}

func TestAgentsParentL1(t *testing.T) {
	env := &Env{
		Site:         SiteCI,
		OverseerName: "jevons",
		AgentsGET: func(ctx context.Context) ([]AgentRow, error) {
			return []AgentRow{
				{Name: "jevons", Status: "running"},
				{Name: "jevons-po", Parent: "jevons", Status: "running"},
				{Name: "jv-selftest", Parent: "jevons-po", Status: "running"},
			}, nil
		},
	}
	res, err := Run(context.Background(), env, RunRequest{Pack: "agents-parent-L1", Site: SiteCI})
	if err != nil {
		t.Fatal(err)
	}
	if res.Single.Grade != GradePass {
		t.Fatalf("%+v", res.Single)
	}
}

func TestAgentsParentL1MissingParentsFail(t *testing.T) {
	env := &Env{
		Site: SiteCI,
		AgentsGET: func(ctx context.Context) ([]AgentRow, error) {
			return []AgentRow{
				{Name: "orphan-a"},
				{Name: "orphan-b"},
			}, nil
		},
	}
	res, err := Run(context.Background(), env, RunRequest{Pack: "agents-parent-L1", Site: SiteCI})
	if err != nil {
		t.Fatal(err)
	}
	if res.Single.Grade != GradeFail {
		t.Fatalf("want fail, got %s", res.Single.Grade)
	}
}

func TestRunAll(t *testing.T) {
	env := &Env{
		Site: SiteCI,
		HealthGET: func(ctx context.Context) (int, map[string]any, error) {
			return 200, map[string]any{"status": "ok"}, nil
		},
		AgentsGET: func(ctx context.Context) ([]AgentRow, error) {
			return []AgentRow{{Name: "jevons"}}, nil
		},
	}
	res, err := Run(context.Background(), env, RunRequest{Pack: "all", Site: SiteCI})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reports) != 3 {
		t.Fatalf("want 3 packs, got %d", len(res.Reports))
	}
	pass, fail, skip, errn := Summarize(res.Reports)
	if fail != 0 || errn != 0 || skip != 0 || pass != 3 {
		t.Fatalf("pass=%d fail=%d skip=%d err=%d", pass, fail, skip, errn)
	}
}

func TestUnknownPack(t *testing.T) {
	_, err := Run(context.Background(), &Env{}, RunRequest{Pack: "nope", Site: SiteCI})
	if err == nil {
		t.Fatal("want error")
	}
}

func TestListPacks(t *testing.T) {
	ps := ListPacks()
	if len(ps) < 3 {
		t.Fatalf("want ≥3 packs, got %d", len(ps))
	}
	ids := map[string]bool{}
	for _, p := range ps {
		ids[p.ID()] = true
	}
	for _, id := range []string{"health-L1", "composer-growth-L2", "agents-parent-L1"} {
		if !ids[id] {
			t.Fatalf("missing pack %s", id)
		}
	}
}

func TestReportJSONShape(t *testing.T) {
	res, err := Run(context.Background(), &Env{
		Site: SiteCI,
		HealthGET: func(ctx context.Context) (int, map[string]any, error) {
			return 200, map[string]any{"status": "ok"}, nil
		},
	}, RunRequest{Pack: "health-L1", Site: SiteCI})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(res.Single)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"schema_version", "site", "pack", "started_at", "finished_at", "grade", "measurements"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing key %s in %s", k, raw)
		}
	}
}
