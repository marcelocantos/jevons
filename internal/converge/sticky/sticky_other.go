// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package sticky

import (
	"errors"
	"fmt"
	"runtime"
)

// ErrUnsupported is returned where no withdrawable OS alert surface exists.
// Reported at construction, not swallowed at fire time, so a daemon on such a
// platform says so once instead of dropping owner alerts silently.
var ErrUnsupported = errors.New("sticky: no withdrawable OS notification surface")

// NewOSNotifier has no implementation off macOS yet. The daemon falls back to
// leaving the human rung unwired, which converge.Actuate reports per fire.
func NewOSNotifier() (Backend, error) {
	return nil, fmt.Errorf("%w on %s", ErrUnsupported, runtime.GOOS)
}
