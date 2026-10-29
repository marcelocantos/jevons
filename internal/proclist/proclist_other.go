// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !unix

package proclist

import "fmt"

func All() ([]Proc, error) {
	return nil, fmt.Errorf("proclist: process listing is unsupported on this OS")
}

func Lookup(int) (Proc, bool, error) {
	return Proc{}, false, fmt.Errorf("proclist: process listing is unsupported on this OS")
}
