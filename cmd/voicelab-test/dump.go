// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// dumpWAV writes pcm (LEI16 24 kHz mono) to path as a standards-
// compliant RIFF/WAVE file. Used to drop response audio per case
// under --dump-wavs PATH so the user can sanity-check by ear.
func dumpWAV(path string, pcm []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	header := make([]byte, 44)
	dataSize := uint32(len(pcm))
	chunkSize := 36 + dataSize
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], chunkSize)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)         // fmt chunk size
	binary.LittleEndian.PutUint16(header[20:22], 1)          // PCM
	binary.LittleEndian.PutUint16(header[22:24], 1)          // mono
	binary.LittleEndian.PutUint32(header[24:28], 24000)      // sample rate
	binary.LittleEndian.PutUint32(header[28:32], 24000*2)    // byte rate
	binary.LittleEndian.PutUint16(header[32:34], 2)          // block align
	binary.LittleEndian.PutUint16(header[34:36], 16)         // bits per sample
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], dataSize)
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := out.Write(header); err != nil {
		return err
	}
	_, err = out.Write(pcm)
	return err
}

// quietHandler wraps a slog.Handler and drops the specific "grok: read
// error" emitted by the readLoop during a clean ctx-cancel shutdown.
// The race is well-known (Close hasn't flipped the closed flag by the
// time conn.Read returns ctx.Canceled), benign, and clutters every
// passing test run. Real connection failures still surface via
// OnError, which the harness wires to its own logger.
type quietHandler struct{ inner slog.Handler }

func (q quietHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return q.inner.Enabled(ctx, lvl)
}
func (q quietHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Message == "grok: read error" {
		return nil
	}
	return q.inner.Handle(ctx, r)
}
func (q quietHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return quietHandler{inner: q.inner.WithAttrs(attrs)}
}
func (q quietHandler) WithGroup(name string) slog.Handler {
	return quietHandler{inner: q.inner.WithGroup(name)}
}
