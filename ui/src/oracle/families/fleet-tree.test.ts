// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { companyOfProvider } from '../../plan/companyMark';
import { modelPrefix } from '../../plan/modelPrefix';
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

  itOracle.todo('T68', 'RHS shows who-started-whom as a relationship tree');
  itOracle.todo('T72', 'RHS reflects the full live agent graph');
  itOracle.todo('T72.1', 'every live fleet agent appears while it exists');
  itOracle.todo('T115', 'root overseer omits ~/.jevons/jevons; asides use description chrome');
  itOracle.todo('T285.2', 'icon menu migrates any agent; ! when its provider is ahead or hot');
  itOracle.todo('T293', 'Grok prefix uses the Grok mark and a condensed version subscript');
  itOracle.todo('T295', 'Claude splat + version drops leading zeros');
  itOracle.todo('T298', 'model subscript is unambiguous — no letter reads as a digit');
  itOracle.todo('T299', 'badges use authentic brand SVGs and true sub-baseline');
  itOracle.todo('T348', 'Claude badges always show condensed version when model is known');
  itOracle.todo('T383', 'fleet-tree selection is sticky across background refresh');
  itOracle.todo('T506', 'model selector is readable and tappable at normal desktop zoom');
  itOracle.todo('T508', 'Bedrock selectors show the Amazon mark before the vendor mark');
});
