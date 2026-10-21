// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package delivery

import "errors"

// ErrNotifyHeldByIntent is returned when the overseer notify queue has
// accepted text but fleet intent refuses to begin a turn. The text stays
// queued. Callers must not report that handoff as delivered.
var ErrNotifyHeldByIntent = errors.New("notify queue held: fleet intent refuses a turn")
