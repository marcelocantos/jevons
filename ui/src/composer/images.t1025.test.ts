// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { UPLOAD_TIMEOUT_MS, UploadError, uploadPastedImage } from './images';

// 🎯T1025: a stalled upload fails, in a named way, instead of hanging until
// the tab is foregrounded again. Each failure kind is what the composer
// reports to the owner, so each is pinned here.

const FILE = new File([new Uint8Array([1, 2, 3])], 'p.png', { type: 'image/png' });
const META = { id: 'abc123', url: '/api/images/abc123', thumb_url: '/api/images/abc123/thumb', marker: '[image: abc123]', width: 4, height: 4 };

/** A fetch that never answers on its own; it rejects only when its signal aborts. */
function stalledFetch() {
  const signals: AbortSignal[] = [];
  const fn = vi.fn((_url: string, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
    const signal = init?.signal;
    if (!signal) return;
    signals.push(signal);
    signal.addEventListener('abort', () => reject(signal.reason ?? new DOMException('aborted', 'AbortError')));
  }));
  return { fn: fn as unknown as typeof fetch, signals };
}

beforeEach(() => { vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] }); });
afterEach(() => { vi.useRealTimers(); });

describe('uploadPastedImage (🎯T1025)', () => {
  it('times out a stalled upload with a timeout error naming the wait', async () => {
    const { fn, signals } = stalledFetch();
    const p = uploadPastedImage(FILE, fn, { timeoutMs: 5_000 });
    const settled = p.then(() => 'resolved', (e: unknown) => e);
    expect(signals).toHaveLength(1);
    expect(signals[0].aborted).toBe(false);
    vi.advanceTimersByTime(4_999);
    expect(signals[0].aborted).toBe(false);
    vi.advanceTimersByTime(1);
    const err = await settled;
    expect(err).toBeInstanceOf(UploadError);
    expect((err as UploadError).kind).toBe('timeout');
    expect((err as UploadError).message).toContain('timed out after 5s');
    expect(signals[0].aborted).toBe(true);
  });

  it('defaults to the product timeout, which is a minute, not forever', async () => {
    const { fn, signals } = stalledFetch();
    const settled = uploadPastedImage(FILE, fn).then(() => 'resolved', (e: unknown) => e);
    expect(UPLOAD_TIMEOUT_MS).toBe(60_000);
    vi.advanceTimersByTime(UPLOAD_TIMEOUT_MS);
    const err = await settled;
    expect((err as UploadError).kind).toBe('timeout');
    expect(signals[0].aborted).toBe(true);
  });

  it('a caller-side cancel aborts the request and reports cancelled, not timeout', async () => {
    const { fn, signals } = stalledFetch();
    const controller = new AbortController();
    const settled = uploadPastedImage(FILE, fn, { signal: controller.signal }).then(() => 'resolved', (e: unknown) => e);
    controller.abort();
    const err = await settled;
    expect((err as UploadError).kind).toBe('cancelled');
    expect(signals[0].aborted).toBe(true);
    // The timeout timer is released with the request.
    expect(vi.getTimerCount()).toBe(0);
  });

  it('an already-aborted signal never starts the request', async () => {
    const { fn, signals } = stalledFetch();
    const controller = new AbortController();
    controller.abort();
    const err = await uploadPastedImage(FILE, fn, { signal: controller.signal }).then(() => 'resolved', (e: unknown) => e);
    expect((err as UploadError).kind).toBe('cancelled');
    expect(fn).not.toHaveBeenCalled();
    expect(signals).toHaveLength(0);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('a server rejection is reported as rejected with the status', async () => {
    const fn = vi.fn().mockResolvedValue({ ok: false, status: 413 }) as unknown as typeof fetch;
    const err = await uploadPastedImage(FILE, fn).then(() => 'resolved', (e: unknown) => e);
    expect((err as UploadError).kind).toBe('rejected');
    expect((err as UploadError).message).toBe('upload 413');
    expect(vi.getTimerCount()).toBe(0);
  });

  it('a network failure is reported as network with the underlying message', async () => {
    const fn = vi.fn().mockRejectedValue(new TypeError('Failed to fetch')) as unknown as typeof fetch;
    const err = await uploadPastedImage(FILE, fn).then(() => 'resolved', (e: unknown) => e);
    expect((err as UploadError).kind).toBe('network');
    expect((err as UploadError).message).toBe('Failed to fetch');
  });

  it('a successful upload maps the server meta and clears its timer', async () => {
    const fn = vi.fn().mockResolvedValue({ ok: true, json: async () => META }) as unknown as typeof fetch;
    const got = await uploadPastedImage(FILE, fn);
    expect(got).toEqual({ id: 'abc123', url: META.url, thumbUrl: META.thumb_url, marker: META.marker, width: 4, height: 4 });
    expect(vi.getTimerCount()).toBe(0);
    const init = (fn as unknown as ReturnType<typeof vi.fn>).mock.calls[0][1] as RequestInit;
    expect(init.signal).toBeInstanceOf(AbortSignal);
    expect(init.method).toBe('POST');
  });
});
