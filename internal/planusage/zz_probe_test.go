package planusage

import (
	"testing"
	"time"
)

func zzProbePf(v float64) *float64 { return &v }

func zzProbeBackend(provider string, weeklyRemaining, sessionRemaining *float64, now time.Time) Backend {
	reset := now.Add(3 * 24 * time.Hour)
	lim := DefaultWeeklyWindowSeconds
	be := Backend{Provider: provider, Status: StatusAvailable, FetchedAt: now}
	if weeklyRemaining != nil {
		used := 100 - *weeklyRemaining
		be.Windows = append(be.Windows, Window{
			Name: WindowWeekly, RemainingPercent: weeklyRemaining, UsedPercent: &used,
			ResetsAt: &reset, LimitWindowSeconds: &lim,
		})
	}
	if sessionRemaining != nil {
		used := 100 - *sessionRemaining
		sreset := now.Add(2 * time.Hour)
		be.Windows = append(be.Windows, Window{
			Name: WindowSession, RemainingPercent: sessionRemaining, UsedPercent: &used,
			ResetsAt: &sreset,
		})
	}
	return be
}

func TestZZProbeSessionLowDestEligible(t *testing.T) {
	now := time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC)
	th := DefaultThresholds()
	low := th.LowRemainingPercent
	// weekly healthy (40% used at mid-week), session at the low threshold
	be := zzProbeBackend("grok", zzProbePf(40), &low, now)
	t.Logf("SessionStatusOf=%v WeeklyBandOf=%v", SessionStatusOf(be, th), WeeklyBandOf(be, now, th))
	t.Logf("DestEligible=%v MintIneligible=%v", DestEligible(be, now, th), MintIneligible(be, now, th))
}
