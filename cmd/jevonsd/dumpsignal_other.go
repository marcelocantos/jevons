// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !unix

package main

import "os"

// dumpSignal has no portable equivalent off unix; nil disables the dump.
var dumpSignal os.Signal
