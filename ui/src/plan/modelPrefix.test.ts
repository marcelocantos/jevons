// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { mergeAgentChrome, modelPrefix, versionOf } from './modelPrefix';

describe('modelPrefix', () => {
  it('condenses Claude family + version (🎯T287 / T302)', () => {
    const p = modelPrefix({ provider: 'claude', model: 'claude-opus-4-5-20250929' });
    expect(p.company).toBe('anthropic');
    expect(p.initial).toBe('O');
    expect(p.version).toBe('4.5');
    expect(p.label).toBe('O4.5');
  });

  it('shows the model id it was given, including on a Cursor seat', () => {
    const p = modelPrefix({ provider: 'cursor', model: 'claude-opus-5' });
    expect(p.company).toBe('cursor');
    expect(p.initial).toBe('O');
    expect(p.version).toBe('5');
    expect(p.label).toBe('O5');
    expect(p.title).toBe('Cursor · claude-opus-5');
  });

  it('condenses Composer on a Cursor seat', () => {
    const p = modelPrefix({ provider: 'cursor', model: 'composer-2.5' });
    expect(p.company).toBe('cursor');
    expect(p.label).toBe('C2.5');
    expect(p.title).toBe('Cursor · composer-2.5');
  });

  it('paints Cursor from provider even with no model id', () => {
    const p = modelPrefix({ provider: 'cursor' });
    expect(p.company).toBe('cursor');
    expect(p.version).toBe('');
    expect(p.label).toBe('');
    expect(p.title).toBe('Cursor');
  });

  it('Grok is bare version, not G4.5', () => {
    const p = modelPrefix({ provider: 'grok', model: 'grok-4.5-build' });
    expect(p.company).toBe('xai');
    expect(p.initial).toBe('');
    expect(p.version).toBe('4.5');
  });

  it('Claude sibling and Cursor sibling both get a company mark', () => {
    const claude = modelPrefix({ provider: 'claude', model: 'claude-opus-4-5' });
    const cursor = modelPrefix({ provider: 'cursor', model: '' });
    expect(claude.company).toBe('anthropic');
    expect(claude.version).toBe('4.5');
    expect(cursor.company).toBe('cursor');
    expect(cursor.label).toBe('');
  });

  it('unknown company paints nothing', () => {
    expect(modelPrefix({}).company).toBe('');
    expect(modelPrefix({ provider: 'mystery-llm' }).company).toBe('');
  });

  it('versionOf reads 4-05 as 4.5', () => {
    expect(versionOf('claude-opus-4-05')).toBe('4.5');
  });
});

describe('mergeAgentChrome', () => {
  it('clears the version when a poll names the provider and no model', () => {
    const cursor = mergeAgentChrome(
      [{ name: 'multimaze2-po', provider: 'cursor', model: 'claude-opus-5' }],
      [{ name: 'multimaze2-po', provider: 'cursor', model: '' }],
    );
    expect(cursor[0].model).toBe('');
    expect(modelPrefix(cursor[0]).label).toBe('');

    const claude = mergeAgentChrome(
      [{ name: 'jevons-po', provider: 'grok', model: 'grok-4.5' }],
      [{ name: 'jevons-po', provider: 'claude', model: '' }],
    );
    expect(claude[0].provider).toBe('claude');
    expect(claude[0].model).toBe('');
    expect(modelPrefix(claude[0]).label).toBe('');
  });

  it('replaces the version when the poll names a new model', () => {
    const next = mergeAgentChrome(
      [{ name: 'w', provider: 'claude', model: 'claude-opus-5' }],
      [{ name: 'w', provider: 'claude', model: 'claude-sonnet-5' }],
    );
    expect(next[0].model).toBe('claude-sonnet-5');
    expect(modelPrefix(next[0]).label).toBe('S5');
  });

  it('holds provider and model when the next poll omits them', () => {
    const prev = [{ name: 'jv-t541', provider: 'cursor', model: 'grok-4.5' }];
    const next = [{ name: 'jv-t541', provider: '', model: '' }];
    expect(mergeAgentChrome(prev, next)[0]).toEqual({
      name: 'jv-t541',
      provider: 'cursor',
      model: 'grok-4.5',
    });
  });
});
