// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import {
  correspondentSuffix,
  formatOverseerStatus,
  optimisticReceived,
  phaseSampleFromUnknown,
  statusBarText,
} from './overseerPhase';

describe('overseerPhase (🎯T555.2)', () => {
  it('maps the closed enum to status-bar copy with no Jevons-is prefix', () => {
    expect(formatOverseerStatus({ phase: 'idle' })).toBe('idle');
    expect(formatOverseerStatus({ phase: 'accepted' })).toBe('received');
    expect(formatOverseerStatus({ phase: 'thinking' })).toBe('thinking');
    expect(formatOverseerStatus({ phase: 'tool' })).toBe('tool');
    expect(formatOverseerStatus({ phase: 'tool', step: 'Read' })).toBe('Read');
    expect(formatOverseerStatus({ phase: 'streaming' })).toBe('writing');
    expect(formatOverseerStatus({ phase: 'streaming', tokens: 12 })).toBe('writing · 12');
    expect(formatOverseerStatus({ phase: 'permission' })).toBe('permission');
    expect(formatOverseerStatus({ phase: 'error' })).toBe('error');
    expect(formatOverseerStatus({ phase: 'stuck' })).toBe('stuck');
    expect(formatOverseerStatus({ phase: 'thinking', correspondent: ['jevons-po'] })).toBe(
      'thinking · answering jevons-po',
    );
    expect(
      formatOverseerStatus({ phase: 'streaming', correspondent: ['jevons-po', 'jv-t555'] }),
    ).toBe('writing · answering jevons-po, jv-t555');
    expect(formatOverseerStatus({ phase: 'idle', correspondent: ['jevons-po'] })).toBe('idle');
    expect(formatOverseerStatus({ phase: 'Jevons is working' })).toBe('working');
    expect(formatOverseerStatus(null)).toBe('idle');
  });

  it('reduces interleaved progress frames and nested history_meta.phase', () => {
    expect(phaseSampleFromUnknown({ type: 'progress', phase: 'accepted', working: true })).toEqual({
      phase: 'accepted',
    });
    expect(
      phaseSampleFromUnknown({
        type: 'progress',
        phase: 'tool',
        step: 'Read',
        correspondent: ['jevons-po'],
      }),
    ).toEqual({ phase: 'tool', step: 'Read', correspondent: ['jevons-po'] });
    expect(phaseSampleFromUnknown({ phase: { phase: 'idle' } })).toEqual({ phase: 'idle' });
    expect(phaseSampleFromUnknown({ working: false })).toBeNull();
    expect(phaseSampleFromUnknown('thinking')).toEqual({ phase: 'thinking' });
  });

  it('paints idle when connected and connecting before the socket is up', () => {
    expect(statusBarText(false, null)).toBe('connecting');
    expect(statusBarText(true, null)).toBe('idle');
    expect(statusBarText(true, { phase: { phase: 'accepted' } })).toBe('received');
    expect(statusBarText(true, { phase: 'stuck' })).toBe('stuck');
    expect(optimisticReceived()).toEqual({ phase: 'accepted' });
  });

  it('T576 correspondent copy names who we are answering, not a destination', () => {
    expect(correspondentSuffix(['jevons-po'])).toBe('answering jevons-po');
    expect(correspondentSuffix(['fleet'])).toBe('answering fleet');
    expect(correspondentSuffix(['jevons-po', 'jv-t555'])).toBe('answering jevons-po, jv-t555');
    expect(correspondentSuffix([])).toBe('');

    const writingFleet = formatOverseerStatus({ phase: 'streaming', correspondent: ['fleet'] });
    expect(writingFleet).toBe('writing · answering fleet');
    expect(writingFleet).not.toBe('writing · fleet');
    expect(looksLikeDestination(writingFleet)).toBe(false);

    const writingPo = formatOverseerStatus({ phase: 'streaming', correspondent: ['jevons-po'] });
    expect(writingPo).toBe('writing · answering jevons-po');
    expect(writingPo).not.toBe('writing · jevons-po');
    expect(looksLikeDestination(writingPo)).toBe(false);

    const owner = formatOverseerStatus({ phase: 'thinking' });
    expect(owner).toBe('thinking');
    expect(looksLikeDestination(owner)).toBe(false);

    // Token count is not a correspondent and must not trip the destination parse.
    expect(looksLikeDestination(formatOverseerStatus({ phase: 'streaming', tokens: 12 }))).toBe(
      false,
    );
  });
});

/** Destination-shaped: "to X", "on behalf of", or phase · name with no answering verb. */
function looksLikeDestination(copy: string): boolean {
  if (/\bto\b/i.test(copy) || /on behalf of/i.test(copy)) return true;
  return /^(received|thinking|writing|tool|permission|error|stuck) · (?!answering\b)[A-Za-z][\w.-]*(?:, [\w.-]+)*$/.test(
    copy,
  );
}
