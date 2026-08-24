// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { degradedBannerText } from './degraded';
import { applyConversationEvent, emptyConversation } from './reduce';

describe('degradedBannerText (🎯T545.6)', () => {
  it('paints a standing overseer-down level and clears when the sample is empty', () => {
    expect(degradedBannerText({ overseer_down: 'the overseer is not running' })).toBe(
      'Cockpit degraded: the overseer is not running',
    );
    expect(degradedBannerText({ overseer_down: '' })).toBe('');
    expect(degradedBannerText({ error: 'send failed' })).toBe('');
  });

  it('a mux send nack does not become the degrade banner', () => {
    let s = emptyConversation();
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'error', body: { error: 'overseer is not running' },
    });
    expect(s.error).toBeNull();
    expect(degradedBannerText(s.meta)).toBe('');
    s = applyConversationEvent(s, {
      v: 1, ch: 'transcript:jevons', t: 'meta',
      body: { older: 0, overseer_down: 'the overseer is not running' },
    });
    expect(degradedBannerText(s.meta)).toBe('Cockpit degraded: the overseer is not running');
  });
});
