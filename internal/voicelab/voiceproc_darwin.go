// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package voicelab

/*
#cgo darwin CFLAGS: -x objective-c -Wno-deprecated-declarations
#cgo darwin LDFLAGS: -framework AudioToolbox -framework CoreAudio -framework AudioUnit -framework Foundation

#include <stdlib.h>
#include "voiceproc_darwin.h"
*/
import "C"

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"
)

// VoiceProcDevice is the macOS equivalent of MalgoDevice, backed by
// the system's VoiceProcessingIO audio unit. AEC, noise suppression,
// and AGC are handled by the audio unit itself — the same path
// FaceTime uses — so speakers won't loop back through the mic.
//
// The C side runs the audio unit and owns two ring buffers (mic and
// speaker). A small Go goroutine pulls from the mic ring at ~20 ms
// cadence and pushes onto the Source channel; Sink.Write copies
// straight into the speaker ring.
type VoiceProcDevice struct {
	handle     *C.voiceproc_t
	captureCh  chan []byte
	stopCh     chan struct{}
	closeOnce  sync.Once
	frameBytes int // one drain pull's worth (~20 ms)
}

// NewVoiceProcDevice opens, initialises, and starts the
// VoiceProcessingIO audio unit at 24 kHz PCM16 mono.
func NewVoiceProcDevice() (*VoiceProcDevice, error) {
	var cErr *C.char
	handle := C.voiceproc_new(C.int(SampleRate), &cErr)
	if handle == nil {
		msg := "voiceproc: init failed"
		if cErr != nil {
			msg = "voiceproc: " + C.GoString(cErr)
		}
		return nil, errors.New(msg)
	}

	const drainEveryMs = 20
	frames := SampleRate * drainEveryMs / 1000
	dev := &VoiceProcDevice{
		handle:     handle,
		captureCh:  make(chan []byte, 64),
		stopCh:     make(chan struct{}),
		frameBytes: frames * BytesPerSample,
	}
	go dev.drainCapture(frames, drainEveryMs)
	return dev, nil
}

func (v *VoiceProcDevice) drainCapture(framesPerPull, intervalMs int) {
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()
	scratch := make([]int16, framesPerPull)
	for {
		select {
		case <-v.stopCh:
			close(v.captureCh)
			return
		case <-ticker.C:
			n := int(C.voiceproc_capture_read(
				v.handle,
				(*C.int16_t)(unsafe.Pointer(&scratch[0])),
				C.int(framesPerPull),
			))
			if n <= 0 {
				continue
			}
			buf := make([]byte, n*BytesPerSample)
			for i := 0; i < n; i++ {
				binary.LittleEndian.PutUint16(buf[i*2:i*2+2], uint16(scratch[i]))
			}
			select {
			case v.captureCh <- buf:
			default:
				// Channel full — drop. The capture goroutine itself
				// runs at the same cadence the consumer expects, so
				// the only way to fill this channel is if the loop's
				// downstream (Grok WS write) is wedged.
			}
		}
	}
}

// Source returns the capture-side audio source. Frames are PCM16 mono
// 24 kHz with AEC, NS, and AGC already applied.
func (v *VoiceProcDevice) Source() AudioSource { return vpSource{v: v} }

// Sink returns the playback-side audio sink. Anything written here
// becomes the AEC reference for the mic path automatically — the
// audio unit handles that internally.
func (v *VoiceProcDevice) Sink() AudioSink { return vpSink{v: v} }

// Close stops the audio unit and releases the C-side state.
func (v *VoiceProcDevice) Close() error {
	v.closeOnce.Do(func() {
		close(v.stopCh)
		// Wait briefly for drainCapture to exit so we don't dispose
		// the C handle while it's mid-read.
		time.Sleep(30 * time.Millisecond)
		C.voiceproc_close(v.handle)
		v.handle = nil
	})
	return nil
}

type vpSource struct{ v *VoiceProcDevice }

func (s vpSource) Frames() <-chan []byte { return s.v.captureCh }
func (s vpSource) Close() error          { return nil }

type vpSink struct{ v *VoiceProcDevice }

func (s vpSink) Write(pcm []byte) error {
	if len(pcm) < 2 {
		return nil
	}
	if s.v.handle == nil {
		return errors.New("voiceproc device closed")
	}
	samples := len(pcm) / 2
	tmp := make([]int16, samples)
	for i := 0; i < samples; i++ {
		tmp[i] = int16(binary.LittleEndian.Uint16(pcm[i*2 : i*2+2]))
	}
	wrote := int(C.voiceproc_playback_write(
		s.v.handle,
		(*C.int16_t)(unsafe.Pointer(&tmp[0])),
		C.int(samples),
	))
	if wrote < samples {
		return fmt.Errorf("voiceproc: playback buffer full (%d/%d frames accepted)", wrote, samples)
	}
	return nil
}

func (s vpSink) Clear() {
	if s.v.handle == nil {
		return
	}
	C.voiceproc_playback_clear(s.v.handle)
}

func (s vpSink) Close() error { return nil }
