// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { decodeMux, encodeMux, transcriptChannel } from './protocol';

describe('mux protocol', () => {
  it('round-trips an envelope', () => {
    const raw = encodeMux('transcript:jevons', 'meta', { older: 0, total: 2 });
    const env = decodeMux(raw);
    expect(env?.ch).toBe('transcript:jevons');
    expect(env?.t).toBe('meta');
    expect(env?.v).toBe(1);
    expect((env?.body as { total: number }).total).toBe(2);
  });

  it('names every agent the same way', () => {
    expect(transcriptChannel('jevons')).toBe('transcript:jevons');
    expect(transcriptChannel('jevons-po')).toBe('transcript:jevons-po');
    expect(transcriptChannel('jv-t537-worker')).toBe('transcript:jv-t537-worker');
  });

  it('rejects junk', () => {
    expect(decodeMux('not json')).toBeNull();
    expect(decodeMux('{"v":2,"t":"hello"}')).toBeNull();
  });
});
