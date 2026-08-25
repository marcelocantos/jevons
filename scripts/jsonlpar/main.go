// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// jsonlpar times serial vs parallel JSONL parse.
//
// Split the file into nprocs byte ranges, seek to each linear guess,
// then scan forward to the next newline so no worker starts mid-line.
// Each worker json.Unmarshals every complete line in its range.
//
//	go run ./scripts/jsonlpar [--path FILE] [--procs N]
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"time"
)

func main() {
	path := flag.String("path", os.ExpandEnv("$HOME/.jevons/chatlog/jevons.jsonl"), "JSONL file")
	procs := flag.Int("procs", runtime.NumCPU(), "worker count")
	flag.Parse()

	st, err := os.Stat(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stat: %v\n", err)
		os.Exit(1)
	}
	size := st.Size()
	fmt.Printf("file=%s size=%.1fMB cpus=%d procs=%d\n", *path, float64(size)/1e6, runtime.NumCPU(), *procs)

	timeOp("serial-newlines", func() (int, error) { return countNewlines(*path) })
	timeOp("serial-parse", func() (int, error) { return parseRange(*path, 0, size) })

	starts, err := segmentStarts(*path, size, *procs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "segments: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("segments=%d\n", len(starts)-1)
	for i := 0; i < len(starts)-1; i++ {
		fmt.Printf("  [%d] %d..%d (%.1fMB)\n", i, starts[i], starts[i+1], float64(starts[i+1]-starts[i])/1e6)
	}

	timeOp("parallel-parse", func() (int, error) { return parseParallel(*path, starts) })
}

func timeOp(name string, fn func() (int, error)) {
	t0 := time.Now()
	n, err := fn()
	d := time.Since(t0)
	if err != nil {
		fmt.Printf("%-16s ERR %v\n", name, err)
		return
	}
	lps := 0.0
	if d > 0 {
		lps = float64(n) / d.Seconds()
	}
	fmt.Printf("%-16s lines=%d dur=%s lines/s=%.0f\n", name, n, d.Round(time.Millisecond), lps)
}

func countNewlines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var buf [1 << 20]byte
	n := 0
	for {
		k, err := f.Read(buf[:])
		for i := 0; i < k; i++ {
			if buf[i] == '\n' {
				n++
			}
		}
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, err
		}
	}
}

// segmentStarts returns n+1 offsets: aligned line starts plus EOF.
func segmentStarts(path string, size int64, n int) ([]int64, error) {
	if n < 1 {
		n = 1
	}
	starts := make([]int64, n+1)
	starts[n] = size
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	for i := 1; i < n; i++ {
		guess := size * int64(i) / int64(n)
		off, err := alignAfterNL(f, guess, size)
		if err != nil {
			return nil, err
		}
		starts[i] = off
	}
	// Drop empty slices if several guesses landed on the same newline.
	out := []int64{starts[0]}
	for i := 1; i < len(starts); i++ {
		if starts[i] > out[len(out)-1] {
			out = append(out, starts[i])
		}
	}
	return out, nil
}

func alignAfterNL(f *os.File, guess, size int64) (int64, error) {
	if guess <= 0 {
		return 0, nil
	}
	if guess >= size {
		return size, nil
	}
	if _, err := f.Seek(guess, io.SeekStart); err != nil {
		return 0, err
	}
	var buf [4096]byte
	for {
		k, err := f.Read(buf[:])
		for i := 0; i < k; i++ {
			if buf[i] == '\n' {
				cur, _ := f.Seek(0, io.SeekCurrent)
				return cur - int64(k) + int64(i) + 1, nil
			}
		}
		if err == io.EOF {
			return size, nil
		}
		if err != nil {
			return 0, err
		}
	}
}

func parseParallel(path string, starts []int64) (int, error) {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		sum  int
		first error
	)
	for i := 0; i < len(starts)-1; i++ {
		lo, hi := starts[i], starts[i+1]
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := parseRange(path, lo, hi)
			mu.Lock()
			sum += n
			if err != nil && first == nil {
				first = err
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return sum, first
}

func parseRange(path string, lo, hi int64) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if lo > 0 {
		if _, err := f.Seek(lo, io.SeekStart); err != nil {
			return 0, err
		}
	}
	r := bufio.NewReaderSize(f, 1<<20)
	n := 0
	pos := lo
	for pos < hi {
		line, err := r.ReadBytes('\n')
		pos += int64(len(line))
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}
		if len(line) > 0 {
			var msg map[string]any
			if jerr := json.Unmarshal(line, &msg); jerr != nil {
				return n, fmt.Errorf("offset %d: %w", pos-int64(len(line)), jerr)
			}
			n++
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
