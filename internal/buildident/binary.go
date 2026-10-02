// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package buildident

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sync"
)

// BinaryIDLen is how many hex characters of the executable's sha256 name a
// build on the wire. 16 hex = 64 bits: collisions between two builds that
// ever meet one client are not a realistic failure.
const BinaryIDLen = 16

var (
	binaryOnce sync.Once
	binaryID   string
)

// Binary is the build id of the running executable (🎯T993): the leading
// BinaryIDLen hex characters of the sha256 of the file os.Executable names,
// computed once per process.
//
// WHY THE BINARY, NOT THE VERSION OR THE COMMIT. Development builds carry
// Version=dev and are made in a snapshot directory with no VCS stamp, so
// neither names a build. restart-jevonsd.sh already decides "identical
// build → no-op" by the binary's sha256 (🎯T218); a client that reloads on
// the same identity therefore reloads exactly when the restart path
// activated something new, and never on a plain bounce of the same bytes.
//
// FAILS TO EMPTY, NEVER TO A GUESS. When the executable cannot be read the
// id is "", which the cockpit treats as unknown and never reloads on. A
// random or per-process stand-in would turn every restart into a reload,
// the exact failure 🎯T993 exists to prevent.
func Binary() string {
	binaryOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		id, err := BinaryOf(exe)
		if err != nil {
			return
		}
		binaryID = id
	})
	return binaryID
}

// BinaryOf is Binary for an arbitrary file: the leading BinaryIDLen hex
// characters of its sha256.
func BinaryOf(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:BinaryIDLen], nil
}
