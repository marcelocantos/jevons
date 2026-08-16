// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin || !cgo

package imagetext

import (
	"context"
	"runtime"
	"time"
)

// Available reports whether a deterministic recogniser is compiled in.
func Available() bool { return false }

// UnavailableReason names why no recogniser is compiled in, in the words that
// reach the owner's transcript.
func UnavailableReason() string {
	return "no OS text recogniser on " + runtime.GOOS + "/" + runtime.GOARCH +
		" (macOS Vision requires darwin with cgo enabled)"
}

// Extract degrades to image-only on a host with no recogniser.
//
// It records WHY, and it records nothing else. The alternative — asking a
// model to describe the image instead — is the substitution this target
// exists to forbid: a fallback description is indistinguishable from a
// measurement once it is in the transcript, and it is wrong far more often
// than it is right.
func Extract(_ context.Context, _, imageID string) Extraction {
	return Extraction{
		Version:     SchemaVersion,
		ImageID:     imageID,
		Source:      SourceUnavailable,
		Degraded:    true,
		Reason:      UnavailableReason(),
		ExtractedAt: time.Now().UTC(),
	}
}
