// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marcelocantos/jevons/internal/idea"
)

// 🎯T325.3 oracle: capture → listed on GET /api/ideas within ceremony.
func TestIdeaCaptureListTriageHTTP(t *testing.T) {
	state := t.TempDir()
	s := New("test", state)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body, _ := json.Marshal(map[string]string{
		"text":     "spark: durable idea so it does not evaporate",
		"source":   "capture",
		"aside_id": "att-spark-1",
	})
	resp, err := http.Post(srv.URL+"/api/ideas", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("capture status %d", resp.StatusCode)
	}
	var rec idea.Record
	if err := json.NewDecoder(resp.Body).Decode(&rec); err != nil {
		t.Fatal(err)
	}
	if rec.ID == "" || rec.Text == "" {
		t.Fatalf("record incomplete: %+v", rec)
	}
	if rec.Disposition != idea.Inbox {
		t.Fatalf("disposition=%s", rec.Disposition)
	}
	if rec.AsideID != "att-spark-1" {
		t.Fatalf("aside_id=%s", rec.AsideID)
	}

	getResp, err := http.Get(srv.URL + "/api/ideas")
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("list status %d", getResp.StatusCode)
	}
	var list ideaListResponse
	if err := json.NewDecoder(getResp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if list.Count != 1 || len(list.Ideas) != 1 {
		t.Fatalf("list=%+v", list)
	}
	if list.Ideas[0].ID != rec.ID {
		t.Fatalf("listed id=%s want %s", list.Ideas[0].ID, rec.ID)
	}

	triBody, _ := json.Marshal(map[string]string{
		"disposition": "file",
		"note":        "product-shaped",
		"target_id":   "T325.3",
	})
	req, err := http.NewRequest(http.MethodPatch, srv.URL+"/api/ideas/"+rec.ID, bytes.NewReader(triBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	triResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer triResp.Body.Close()
	if triResp.StatusCode != http.StatusOK {
		t.Fatalf("triage status %d", triResp.StatusCode)
	}
	var tri struct {
		idea.Record
		NextCeremony string `json:"next_ceremony"`
	}
	if err := json.NewDecoder(triResp.Body).Decode(&tri); err != nil {
		t.Fatal(err)
	}
	if tri.Disposition != idea.File {
		t.Fatalf("after triage disposition=%s", tri.Disposition)
	}
	if tri.TargetID != "T325.3" {
		t.Fatalf("target_id=%s", tri.TargetID)
	}
	if tri.NextCeremony == "" {
		t.Fatal("next_ceremony empty")
	}

	// Empty text rejected.
	bad, _ := json.Marshal(map[string]string{"text": "  "})
	badResp, err := http.Post(srv.URL+"/api/ideas", "application/json", bytes.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}
	defer badResp.Body.Close()
	if badResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty text status %d want 400", badResp.StatusCode)
	}
}

func TestIdeaCaptureHoldLifeDomain(t *testing.T) {
	state := t.TempDir()
	s := New("test", state)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body, _ := json.Marshal(map[string]string{
		"text":   "blood pressure log",
		"source": "idea",
		"domain": "health",
	})
	resp, err := http.Post(srv.URL+"/api/ideas", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var rec idea.Record
	if err := json.NewDecoder(resp.Body).Decode(&rec); err != nil {
		t.Fatal(err)
	}
	if rec.Disposition != idea.Hold {
		t.Fatalf("health domain → hold, got %s", rec.Disposition)
	}
}
