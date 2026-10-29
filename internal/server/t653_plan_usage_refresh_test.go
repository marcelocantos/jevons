// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// T1224 supersedes T653's forced-refresh behavior: every view request only
// reads the latest subscription result, even if an old client sends refresh.
func TestT653LegacyRefreshParameterIsReadOnly(t *testing.T) {
	s := &Server{}
	raw := json.RawMessage(`{"pending":true}`)
	s.SetPlanUsageSource(func() any { return raw })
	for _, flag := range []string{"", "0", "1", "true", "false"} {
		rr := httptest.NewRecorder()
		s.handlePlanUsage(rr, httptest.NewRequest(http.MethodGet, "/api/plan-usage?refresh="+flag, nil))
		if rr.Body.String() != string(raw) {
			t.Fatalf("refresh=%s returned %s", flag, rr.Body.String())
		}
	}
}
