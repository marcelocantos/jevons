package planusage

import (
	"testing"
	"time"
)

func TestZZProbeSessionLowDestEligible(t *testing.T) {
	now := time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC)
	th := DefaultThresholds()
	low := th.LowRemainingPercent
	// weekly healthy (40% used at mid-week), session at the low threshold
	be := t583Backend("grok", t583pf(40), &low, now)
	t.Logf("SessionStatusOf=%v WeeklyBandOf=%v", SessionStatusOf(be, th), WeeklyBandOf(be, now, th))
	t.Logf("DestEligible=%v MintIneligible=%v", DestEligible(be, now, th), MintIneligible(be, now, th))
}
