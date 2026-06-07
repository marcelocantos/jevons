// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

#include "voiceproc_darwin.h"

#include <AudioToolbox/AudioToolbox.h>
#include <AudioUnit/AudioUnit.h>
#include <CoreAudio/CoreAudio.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>

// Ring buffer of int16_t samples, lock-protected. The audio thread
// is supposed to be lock-free, but for our buffer sizes (~1s) and
// CB cadence (256 frames @ 24 kHz ≈ 11 ms) the brief lock window is
// well below the audio-stutter threshold. If profiling shows issues
// we can switch to atomic head/tail later — the API surface holds.
typedef struct {
    int16_t *buf;
    int cap;
    int head; // next read position
    int tail; // next write position
    int count;
    pthread_mutex_t mu;
} ring_t;

static void ring_init(ring_t *r, int cap) {
    r->buf = (int16_t *)calloc((size_t)cap, sizeof(int16_t));
    r->cap = cap;
    r->head = r->tail = r->count = 0;
    pthread_mutex_init(&r->mu, NULL);
}

static void ring_free(ring_t *r) {
    if (r->buf) {
        free(r->buf);
        r->buf = NULL;
    }
    pthread_mutex_destroy(&r->mu);
}

// ring_push writes up to `n` samples, returning the number written.
// Drops on overflow (newer samples won't overwrite older — the older
// audio loses; useful when the consumer is slow, common in init).
static int ring_push(ring_t *r, const int16_t *src, int n) {
    pthread_mutex_lock(&r->mu);
    int space = r->cap - r->count;
    int wrote = n < space ? n : space;
    for (int i = 0; i < wrote; i++) {
        r->buf[r->tail] = src[i];
        r->tail = (r->tail + 1) % r->cap;
    }
    r->count += wrote;
    pthread_mutex_unlock(&r->mu);
    return wrote;
}

static int ring_pop(ring_t *r, int16_t *dst, int n) {
    pthread_mutex_lock(&r->mu);
    int avail = r->count;
    int read = n < avail ? n : avail;
    for (int i = 0; i < read; i++) {
        dst[i] = r->buf[r->head];
        r->head = (r->head + 1) % r->cap;
    }
    r->count -= read;
    pthread_mutex_unlock(&r->mu);
    return read;
}

static void ring_clear(ring_t *r) {
    pthread_mutex_lock(&r->mu);
    r->head = r->tail = r->count = 0;
    pthread_mutex_unlock(&r->mu);
}

struct voiceproc {
    AudioUnit unit;
    ring_t cap_ring; // mic → app
    ring_t pb_ring;  // app → speaker
    int sample_rate;
    // Scratch buffer for AudioUnitRender in the input callback. Sized
    // generously; reallocated on the rare beat where the unit asks
    // for more frames than expected.
    int16_t *render_scratch;
    int render_scratch_frames;
    pthread_mutex_t scratch_mu;
};

static OSStatus input_cb(void *refCon,
                         AudioUnitRenderActionFlags *flags,
                         const AudioTimeStamp *ts,
                         UInt32 bus,
                         UInt32 frames,
                         AudioBufferList *unusedBufferList) {
    (void)unusedBufferList;
    struct voiceproc *vp = (struct voiceproc *)refCon;

    pthread_mutex_lock(&vp->scratch_mu);
    if ((int)frames > vp->render_scratch_frames) {
        free(vp->render_scratch);
        vp->render_scratch_frames = (int)frames * 2;
        vp->render_scratch = (int16_t *)calloc(
            (size_t)vp->render_scratch_frames, sizeof(int16_t));
    }
    int16_t *scratch = vp->render_scratch;
    pthread_mutex_unlock(&vp->scratch_mu);

    AudioBufferList bl;
    bl.mNumberBuffers = 1;
    bl.mBuffers[0].mNumberChannels = 1;
    bl.mBuffers[0].mDataByteSize = frames * sizeof(int16_t);
    bl.mBuffers[0].mData = scratch;

    OSStatus err = AudioUnitRender(vp->unit, flags, ts, 1, frames, &bl);
    if (err != noErr) {
        return err;
    }

    ring_push(&vp->cap_ring, scratch, (int)frames);
    return noErr;
}

static OSStatus output_cb(void *refCon,
                          AudioUnitRenderActionFlags *flags,
                          const AudioTimeStamp *ts,
                          UInt32 bus,
                          UInt32 frames,
                          AudioBufferList *bufferList) {
    (void)flags; (void)ts; (void)bus;
    struct voiceproc *vp = (struct voiceproc *)refCon;

    int16_t *out = (int16_t *)bufferList->mBuffers[0].mData;
    int popped = ring_pop(&vp->pb_ring, out, (int)frames);
    for (int i = popped; i < (int)frames; i++) {
        out[i] = 0;
    }
    return noErr;
}

voiceproc_t *voiceproc_new(int sample_rate, const char **err_out) {
    AudioComponentDescription desc;
    desc.componentType = kAudioUnitType_Output;
    desc.componentSubType = kAudioUnitSubType_VoiceProcessingIO;
    desc.componentManufacturer = kAudioUnitManufacturer_Apple;
    desc.componentFlags = 0;
    desc.componentFlagsMask = 0;

    AudioComponent comp = AudioComponentFindNext(NULL, &desc);
    if (!comp) {
        if (err_out) *err_out = "VoiceProcessingIO component not found";
        return NULL;
    }

    AudioUnit unit;
    OSStatus err = AudioComponentInstanceNew(comp, &unit);
    if (err != noErr) {
        if (err_out) *err_out = "AudioComponentInstanceNew failed";
        return NULL;
    }

    // Enable input on bus 1 (mic) and output on bus 0 (speaker).
    UInt32 enable = 1;
    err = AudioUnitSetProperty(unit, kAudioOutputUnitProperty_EnableIO,
                               kAudioUnitScope_Input, 1,
                               &enable, sizeof(enable));
    if (err != noErr) {
        AudioComponentInstanceDispose(unit);
        if (err_out) *err_out = "EnableIO input failed";
        return NULL;
    }
    err = AudioUnitSetProperty(unit, kAudioOutputUnitProperty_EnableIO,
                               kAudioUnitScope_Output, 0,
                               &enable, sizeof(enable));
    if (err != noErr) {
        AudioComponentInstanceDispose(unit);
        if (err_out) *err_out = "EnableIO output failed";
        return NULL;
    }

    AudioStreamBasicDescription fmt;
    memset(&fmt, 0, sizeof(fmt));
    fmt.mSampleRate = (Float64)sample_rate;
    fmt.mFormatID = kAudioFormatLinearPCM;
    fmt.mFormatFlags = kAudioFormatFlagIsSignedInteger | kAudioFormatFlagIsPacked;
    fmt.mFramesPerPacket = 1;
    fmt.mChannelsPerFrame = 1;
    fmt.mBitsPerChannel = 16;
    fmt.mBytesPerPacket = 2;
    fmt.mBytesPerFrame = 2;

    // Output scope of input bus (1) — what the host sees coming out
    // of the mic (after AEC). Input scope of output bus (0) — what
    // the host writes into the speaker.
    err = AudioUnitSetProperty(unit, kAudioUnitProperty_StreamFormat,
                               kAudioUnitScope_Output, 1,
                               &fmt, sizeof(fmt));
    if (err != noErr) {
        AudioComponentInstanceDispose(unit);
        if (err_out) *err_out = "StreamFormat (mic out) failed";
        return NULL;
    }
    err = AudioUnitSetProperty(unit, kAudioUnitProperty_StreamFormat,
                               kAudioUnitScope_Input, 0,
                               &fmt, sizeof(fmt));
    if (err != noErr) {
        AudioComponentInstanceDispose(unit);
        if (err_out) *err_out = "StreamFormat (spk in) failed";
        return NULL;
    }

    voiceproc_t *vp = (voiceproc_t *)calloc(1, sizeof(voiceproc_t));
    vp->unit = unit;
    vp->sample_rate = sample_rate;

    // Ring sizes: 2 s of audio at sample_rate, mono, int16.
    // Generous enough that nothing drops during normal latency
    // hiccups; not so big that buffer-bloat hides real issues.
    ring_init(&vp->cap_ring, sample_rate * 2);
    ring_init(&vp->pb_ring, sample_rate * 2);
    pthread_mutex_init(&vp->scratch_mu, NULL);
    vp->render_scratch_frames = 1024;
    vp->render_scratch = (int16_t *)calloc(
        (size_t)vp->render_scratch_frames, sizeof(int16_t));

    AURenderCallbackStruct in_cb;
    in_cb.inputProc = input_cb;
    in_cb.inputProcRefCon = vp;
    err = AudioUnitSetProperty(unit, kAudioOutputUnitProperty_SetInputCallback,
                               kAudioUnitScope_Global, 0,
                               &in_cb, sizeof(in_cb));
    if (err != noErr) {
        AudioComponentInstanceDispose(unit);
        ring_free(&vp->cap_ring);
        ring_free(&vp->pb_ring);
        free(vp->render_scratch);
        free(vp);
        if (err_out) *err_out = "SetInputCallback failed";
        return NULL;
    }

    AURenderCallbackStruct out_cb;
    out_cb.inputProc = output_cb;
    out_cb.inputProcRefCon = vp;
    err = AudioUnitSetProperty(unit, kAudioUnitProperty_SetRenderCallback,
                               kAudioUnitScope_Input, 0,
                               &out_cb, sizeof(out_cb));
    if (err != noErr) {
        AudioComponentInstanceDispose(unit);
        ring_free(&vp->cap_ring);
        ring_free(&vp->pb_ring);
        free(vp->render_scratch);
        free(vp);
        if (err_out) *err_out = "SetRenderCallback failed";
        return NULL;
    }

    err = AudioUnitInitialize(unit);
    if (err != noErr) {
        AudioComponentInstanceDispose(unit);
        ring_free(&vp->cap_ring);
        ring_free(&vp->pb_ring);
        free(vp->render_scratch);
        free(vp);
        if (err_out) *err_out = "AudioUnitInitialize failed";
        return NULL;
    }

    err = AudioOutputUnitStart(unit);
    if (err != noErr) {
        AudioUnitUninitialize(unit);
        AudioComponentInstanceDispose(unit);
        ring_free(&vp->cap_ring);
        ring_free(&vp->pb_ring);
        free(vp->render_scratch);
        free(vp);
        if (err_out) *err_out = "AudioOutputUnitStart failed";
        return NULL;
    }

    return vp;
}

void voiceproc_close(voiceproc_t *vp) {
    if (!vp) return;
    if (vp->unit) {
        AudioOutputUnitStop(vp->unit);
        AudioUnitUninitialize(vp->unit);
        AudioComponentInstanceDispose(vp->unit);
        vp->unit = NULL;
    }
    ring_free(&vp->cap_ring);
    ring_free(&vp->pb_ring);
    pthread_mutex_lock(&vp->scratch_mu);
    free(vp->render_scratch);
    vp->render_scratch = NULL;
    pthread_mutex_unlock(&vp->scratch_mu);
    pthread_mutex_destroy(&vp->scratch_mu);
    free(vp);
}

int voiceproc_capture_read(voiceproc_t *vp, int16_t *out, int frames) {
    if (!vp || !out) return 0;
    return ring_pop(&vp->cap_ring, out, frames);
}

int voiceproc_playback_write(voiceproc_t *vp, const int16_t *in, int frames) {
    if (!vp || !in) return 0;
    return ring_push(&vp->pb_ring, in, frames);
}

void voiceproc_playback_clear(voiceproc_t *vp) {
    if (!vp) return;
    ring_clear(&vp->pb_ring);
}
