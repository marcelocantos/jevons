// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
//
// VoiceProcessingIO bridge for voicelab: opens the same AudioUnit
// FaceTime uses (kAudioUnitSubType_VoiceProcessingIO), which does
// AEC, noise suppression, and AGC natively because it owns both the
// capture and render sides and uses the playback signal as its echo
// reference automatically.

#ifndef VOICEPROC_DARWIN_H
#define VOICEPROC_DARWIN_H

#include <stdint.h>

typedef struct voiceproc voiceproc_t;

// voiceproc_new opens, initialises, and starts a VoiceProcessingIO
// audio unit at 24 kHz PCM16 mono. Returns NULL on failure; on
// failure, *err_out is set to a static error string (do not free).
voiceproc_t *voiceproc_new(int sample_rate, const char **err_out);

// voiceproc_close stops the audio unit and releases all resources.
// Safe to call once per voiceproc_new; subsequent calls are no-ops.
void voiceproc_close(voiceproc_t *vp);

// voiceproc_capture_read pulls up to `frames` PCM16 mono samples
// from the echo-cancelled mic ring buffer into `out`. Returns the
// actual number of samples written (may be less than requested if
// the buffer is empty). Safe to call from any goroutine.
int voiceproc_capture_read(voiceproc_t *vp, int16_t *out, int frames);

// voiceproc_playback_write pushes up to `frames` PCM16 mono samples
// into the speaker queue. Returns the number of samples accepted
// (may be less than requested if the queue is full — caller can
// retry, drop, or treat as backpressure).
int voiceproc_playback_write(voiceproc_t *vp, const int16_t *in, int frames);

// voiceproc_playback_clear discards all queued playback audio. Used
// for barge-in: when the user starts a new utterance, cut Grok off
// mid-sentence.
void voiceproc_playback_clear(voiceproc_t *vp);

#endif // VOICEPROC_DARWIN_H
