// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package portguard keeps the journey isolate off the daily-driver port.
// This is harness safety, not a user journey.
package portguard

import "fmt"

// DailyPort is the live owner-driver bind (Universe A).
const DailyPort = 13705

// DefaultPort is the default Universe B isolate bind.
const DefaultPort = 13715

// RefuseDaily returns an error when p is DailyPort so the journey suite
// never binds the owner stream.
func RefuseDaily(p int) error {
	if p == DailyPort {
		return fmt.Errorf("refusing port %d (daily-driver); use %d or -port 0", DailyPort, DefaultPort)
	}
	return nil
}
