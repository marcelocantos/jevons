package planusage

import (
	"encoding/json"
	"fmt"
	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestT1284WeekendHeavyHistoryAndThinFallback(t *testing.T) {
	reset := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC) // Monday; prior week Mon..Sun
	var samples []burnSample
	for week := 0; week < 3; week++ {
		r := reset.AddDate(0, 0, -7*week)
		remaining := 100.0
		for day := 0; day <= 7; day++ {
			if day > 0 {
				if day == 6 || day == 7 {
					remaining -= 30
				} else {
					remaining -= 8
				}
			}
			samples = append(samples, burnSample{provider: "grok", window: "weekly", reset: r, at: r.AddDate(0, 0, day-7), remaining: remaining, canonical: true})
		}
	}
	p := calculateBurnProfiles(samples, reset.Add(time.Hour))["grok/weekly"]
	if p.Fallback || p.Windows != 3 {
		t.Fatalf("profile: %+v", p)
	}
	if p.Shares[0]+p.Shares[6] < 0.59 {
		t.Fatalf("weekend must dominate: %+v", p)
	}
	snap := AttachBurnProfiles(Snapshot{Backends: []Backend{{Provider: "grok", Windows: []Window{{Name: "weekly"}, {Name: "monthly"}}}}}, map[string]BurnProfile{"grok/weekly": p})
	if snap.Backends[0].Windows[0].BurnProfile.Windows != 3 || !snap.Backends[0].Windows[1].BurnProfile.Fallback {
		t.Fatalf("wire: %+v", snap)
	}
	thin := calculateBurnProfiles(samples[:8], reset.Add(time.Hour))["grok/weekly"]
	if thin.Windows != 1 || !thin.Fallback {
		t.Fatalf("thin history: %+v", thin)
	}
	if math.Abs(p.Shares[0]+p.Shares[6]-0.6) > 0.01 {
		t.Fatalf("weekend proportion: %+v", p)
	}
}

func TestT1284ProfileMinedFromReplicaHistory(t *testing.T) {
	state := t.TempDir()
	replica, err := controlplane.OpenReplica(filepath.Join(state, controlplane.JevonsDBFile))
	if err != nil {
		t.Fatal(err)
	}
	defer replica.Close()
	if _, err = OpenReplicaOverrides(replica, state); err != nil {
		t.Fatal(err)
	}
	if err = replica.Write(func(db *sqlpipe.Database) error {
		return db.Exec(`CREATE TABLE IF NOT EXISTS claudia_plan_usage_history
   (id INTEGER PRIMARY KEY, provider TEXT, observed_at TEXT, reading_json TEXT)`)
	}); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 3; n++ {
		reset := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -7*n)
		for day := 0; day <= 7; day++ {
			rem := 100 - 8*day
			if day >= 6 {
				rem -= 22 * (day - 5)
			}
			atTime := reset.AddDate(0, 0, day-7)
			if day == 7 {
				atTime = atTime.Add(-time.Second)
			}
			at := atTime.Format(time.RFC3339)
			raw := fmt.Sprintf(`{"windows":[{"name":"weekly","resets_at":%q,"remaining_percent":%d}]}`, reset.Format(time.RFC3339), rem)
			if err = replica.Write(func(db *sqlpipe.Database) error {
				return db.Exec("INSERT INTO claudia_plan_usage_history(provider,observed_at,reading_json) VALUES (?,?,?)", "grok", at, raw)
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The mining path reads the replica and is reached before the HTTP
	// source attaches profiles; this is not merely a pure math test.
	got := MineBurnProfiles(replica)["grok/weekly"]
	if got.Windows != 3 || got.Fallback || got.Shares[0]+got.Shares[6] < 0.5 {
		t.Fatalf("replica profile: %+v", got)
	}
}

func TestT1284WirePreservesProjectionFields(t *testing.T) {
	raw := json.RawMessage(`{"pending":false,"backends":[{"provider":"grok","success_at":"2026-10-29T00:00:00Z","windows":[{"name":"weekly"},{"name":"monthly"}]}]}`)
	result := WithBurnProfilesJSON(raw, map[string]BurnProfile{"grok/weekly": {Windows: 3, Shares: [7]float64{0.3, 0.08, 0.08, 0.08, 0.08, 0.08, 0.3}}})
	var got struct {
		Backends []struct {
			SuccessAt string `json:"success_at"`
			Windows   []struct {
				BurnProfile BurnProfile `json:"burn_profile"`
			} `json:"windows"`
		} `json:"backends"`
	}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	if got.Backends[0].SuccessAt == "" || got.Backends[0].Windows[0].BurnProfile.Windows != 3 || !got.Backends[0].Windows[1].BurnProfile.Fallback {
		t.Fatalf("wire: %s", result)
	}
}
