// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { conversationWorkdir, selectedWorkdir, type TargetAskHost } from './targetAskContext';
import { chromeModel } from './targetAsk';

describe('T1006 conversation workdir for context tabs', () => {
  const host: TargetAskHost = {
    agents: [
      { name: 'jevons', workdir: '/Users/marcelo/work/github.com/marcelocantos/jevons' },
      { name: 'yourworld2-po', workdir: '/Users/marcelo/work/github.com/squz/yourworld2' },
    ],
    selectedAgent: 'yourworld2-po',
  };

  it('selectedWorkdir follows the tree selection', () => {
    expect(selectedWorkdir(host)).toContain('yourworld2');
  });

  it('conversationWorkdir prefers the seat, not the tree selection', () => {
    expect(conversationWorkdir(host, 'jevons')).toContain('marcelocantos/jevons');
    expect(conversationWorkdir(host, 'jevons')).not.toContain('yourworld2');
  });

  it('chromeModel on jevons seat does not paint yourworld2 when tree selects it', () => {
    const text = 'Please decide 🎯T12 needs-owner for the cockpit.';
    const wrong = chromeModel({
      text,
      role: 'assistant',
      agents: host.agents,
      workdir: selectedWorkdir(host),
    });
    const right = chromeModel({
      text,
      role: 'assistant',
      agents: host.agents,
      workdir: conversationWorkdir(host, 'jevons'),
    });
    // When chrome shows, the selected-tree path would drag yourworld2 into context.
    if (wrong.show) {
      expect(String(wrong.contextText || wrong.label || '')).toMatch(/yourworld|squz/i);
    }
    if (right.show) {
      expect(String(right.contextText || right.label || '')).not.toMatch(/yourworld2/i);
    }
  });
});
