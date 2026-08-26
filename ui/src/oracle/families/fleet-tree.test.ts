// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { isStateDirOverseerHome } from '../../fleet/rowModel';
import { companyOfProvider } from '../../plan/companyMark';
import { mergeAgentChrome, modelPrefix, versionOf } from '../../plan/modelPrefix';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('fleet-tree'), () => {
  itOracle('T287', 'fleet badge is company + condensed model prefix', () => {
    const p = modelPrefix({ provider: 'claude', model: 'claude-fable-5' });
    expect(p.company).toBe('anthropic');
    expect(p.label).toBe('F5');
  });

  itOracle('T507', 'Codex maps to the OpenAI/ChatGPT company mark', () => {
    expect(companyOfProvider('codex')).toBe('openai');
  });

  itOracle('T115', 'root overseer omits ~/.jevons/jevons; asides use description chrome', () => {
    expect(isStateDirOverseerHome('/Users/x/.jevons/jevons', 'jevons')).toBe(true);
    expect(isStateDirOverseerHome('/Users/x/work/jevons', 'jevons')).toBe(false);
  });

  itOracle('T293', 'Grok prefix uses the Grok mark and a condensed version subscript', () => {
    const p = modelPrefix({ provider: 'grok', model: 'grok-4.5-build' });
    expect(p.company).toBe('xai');
    expect(p.initial).toBe('');
    expect(p.version).toBe('4.5');
    expect(p.label).toBe('4.5');
  });

  itOracle('T295', 'Claude splat + version drops leading zeros', () => {
    expect(versionOf('claude-opus-4-05')).toBe('4.5');
    const p = modelPrefix({ provider: 'claude', model: 'claude-sonnet-4-05' });
    expect(p.label).not.toMatch(/0[0-9]/);
  });

  itOracle('T298', 'model subscript is unambiguous — no letter reads as a digit', () => {
    const p = modelPrefix({ provider: 'claude', model: 'claude-opus-4-5' });
    expect(p.initial).toBe('O');
    expect(p.version).toBe('4.5');
    expect(p.label).toBe('O4.5');
    expect(/[Il]/.test(p.version)).toBe(false);
  });

  itOracle('T348', 'Claude badges always show condensed version when model is known', () => {
    const p = modelPrefix({ provider: 'claude', model: 'claude-sonnet-4-5' });
    expect(p.version).toBe('4.5');
    expect(p.label).toMatch(/4\.5/);
  });

  itOracle('T383', 'fleet-tree selection is sticky across background refresh', () => {
    const prev = [{ name: 'jv-t383', provider: 'cursor', model: 'grok-4.5' }];
    const next = [{ name: 'jv-t383', provider: '', model: '' }];
    expect(mergeAgentChrome(prev, next)[0].provider).toBe('cursor');
  });

  itOracle('T508', 'Bedrock selectors show the Amazon mark before the vendor mark', () => {
    expect(companyOfProvider('bedrock')).toBe('anthropic');
  });

  itOracle.skip('T68', 'RHS shows who-started-whom as a relationship tree', 'journey is the arbiter (J25)');
  itOracle.skip('T72', 'RHS reflects the full live agent graph', 'journey is the arbiter (J25)');
  itOracle.skip('T72.1', 'every live fleet agent appears while it exists', 'journey is the arbiter (J25)');
  itOracle.skip('T285.2', 'icon menu migrates any agent; ! when its provider is ahead or hot', 'named residual: pixel-identical chrome');
  itOracle.skip('T299', 'badges use authentic brand SVGs and true sub-baseline', 'named residual: pixel-identical chrome');
  itOracle.skip('T506', 'model selector is readable and tappable at normal desktop zoom', 'named residual: pixel-identical chrome');
});
