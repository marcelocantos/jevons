// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { clipClassName, expandTabChevron, shouldClip } from '../../conversation/clip';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('clip'), () => {
  itOracle(['T55', 'T77'], 'tall content clips; short does not', () => {
    expect(shouldClip(400)).toBe(true);
    expect(shouldClip(100)).toBe(false);
    expect(shouldClip(224)).toBe(false);
    expect(shouldClip(226)).toBe(true);
  });

  itOracle('T106', 'clipped class and pocket-tab chevrons match vanilla glyphs', () => {
    expect(clipClassName('bubble bubble-user', 400)).toContain('msg-clipped');
    expect(clipClassName('bubble bubble-user', 80)).toBe('bubble bubble-user');
    expect(expandTabChevron(false)).toBe('\u25BE');
    expect(expandTabChevron(true)).toBe('\u25B4');
  });

  itOracle.todo('T66', 'latest assistant starts expanded (same rule as latest user)');
  itOracle.todo('T166', 'expanded tall bubble clears last line above the collapse tab');
  itOracle.todo('T246', 'new messages stay expanded until scrolled out of view');
  itOracle.todo('T261', 'in-view messages near the end never collapse');
  itOracle.todo('T480', 'main and sidebar Transcript use the same size-clip');
});
