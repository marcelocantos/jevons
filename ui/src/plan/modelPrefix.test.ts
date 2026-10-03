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

  it('shows the Claude model running on a Cursor seat', () => {
    const p = modelPrefix({ provider: 'cursor', model: 'claude-opus-5' });
    expect(p.company).toBe('cursor');
    expect(p.initial).toBe('O');
    expect(p.version).toBe('5');
    expect(p.label).toBe('O5');
    expect(p.title).toBe('Cursor · claude-opus-5');
  });

  it('paints Cursor from provider even with no model id', () => {
    const p = modelPrefix({ provider: 'cursor' });
    expect(p.company).toBe('cursor');
    expect(p.version).toBe('');
    expect(p.label).toBe('');
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

  it.each([
    ['GPT 6.1 Sol', '6.1S'],
    ['gpt-6.1-astra', '6.1A'],
    ['gpt-6.1-luna', '6.1L'],
    ['gpt-6.1-spark', '6.1Sp'],
    ['gpt-6.1', '6.1'],
    ['gpt-6.1-codex', '6.1'],
    ['gpt-6.1-solar', '6.1'],
    ['gpt-5.3-codex-spark', '5.3'],
  ])('condenses GPT model %s as %s', (model, label) => {
    const p = modelPrefix({ provider: 'codex', model });
    expect(p.company).toBe('openai');
    expect(p.label).toBe(label);
    expect(p.initial).toBe('');
    expect(p.version + p.flavour).toBe(label);
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
