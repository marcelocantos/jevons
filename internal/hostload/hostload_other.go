// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin && !linux

package hostload

import (
	"runtime"
	"time"
)

// readPlatform reports the reading as unavailable on platforms with no
// implementation. Admission then treats host load as an unknown dimension,
// which contributes no pressure — the pre-🎯T463 behaviour, named rather than
// silently assumed.
func readPlatform() Sample {
	return Sample{
		Source: runtime.GOOS,
		Err:    "host load reading not implemented on " + runtime.GOOS,
		At:     time.Now(),
	}
}
