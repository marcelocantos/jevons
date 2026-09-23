// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/claudia"
)

// attachCursorAPIUsage adds Cursor's named-model API bucket when the
// plan-usage evaluator only published the blended total.
//
// The released broker still reports totalPercentUsed. Named models are
// refused once apiPercentUsed hits 100, while that blend can sit near half.
// The raw store already kept the vendor body (claudia 🎯T84), so the cockpit
// can show the bucket without a second request. A reading that already
// carries an API window is left alone.
func attachCursorAPIUsage(readings []claudia.PlanUsage, dir string) []claudia.PlanUsage {
	if dir == "" {
		return readings
	}
	used, ok := latestCursorAPIPercent(dir)
	if !ok {
		return readings
	}
	for i, reading := range readings {
		if !strings.EqualFold(string(reading.Provider), string(claudia.ProviderCursor)) {
			continue
		}
		if cursorReadingHasAPI(reading) {
			return readings
		}
		base := claudia.PlanWindow{Name: claudia.PlanWindowAPI}
		if len(reading.Windows) > 0 {
			base = reading.Windows[0]
		}
		// A copied month window would file this figure under "monthly"
		// and the sparkline would draw the blend. The bucket has its own name.
		base.Name = claudia.PlanWindowAPI
		base.Model = "API"
		u := used
		rem := 100 - used
		base.UsedPercent = &u
		base.RemainingPercent = &rem
		reading.Windows = append(reading.Windows, base)
		readings[i] = reading
		return readings
	}
	return readings
}

func cursorReadingHasAPI(reading claudia.PlanUsage) bool {
	for _, w := range reading.Windows {
		if strings.EqualFold(strings.TrimSpace(w.Model), "API") {
			return true
		}
	}
	return false
}

func latestCursorAPIPercent(dir string) (float64, bool) {
	payloads := claudia.ReadPlanRawPayloads(dir, claudia.ProviderCursor)
	for i := len(payloads) - 1; i >= 0; i-- {
		used, ok := cursorAPIPercent([]byte(payloads[i].Body))
		if ok {
			return used, true
		}
	}
	return 0, false
}

func cursorAPIPercent(body []byte) (float64, bool) {
	var doc struct {
		PlanUsage *struct {
			APIPercentUsed *float64 `json:"apiPercentUsed"`
		} `json:"planUsage"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.PlanUsage == nil || doc.PlanUsage.APIPercentUsed == nil {
		return 0, false
	}
	used := *doc.PlanUsage.APIPercentUsed
	if used < 0 || used > 100 {
		return 0, false
	}
	return used, true
}

func cursorPlanCacheDir() string {
	if env := strings.TrimSpace(os.Getenv("CLAUDIA_PLAN_CACHE")); env != "" {
		return env
	}
	root, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(root, "claudia", "plan-usage")
}
