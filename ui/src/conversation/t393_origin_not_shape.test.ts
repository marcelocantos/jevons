// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { displayRows } from './display';
import { bubblePaintsMarkdown } from './paint';

// 🎯T393: the RHS inspect transcript paints a turn by its wire-carried
// origin, not by whether its body looks like markdown. Pin both directions
// on the same shared projection (displayRows) the sidebar actually renders
// from, with inspect=true (the RHS surface).

describe('T393 sidebar paint keys on origin not body shape', () => {
  it('an owner-typed turn with markdown-shaped body (hashes, pipes, asterisks) stays verbatim', () => {
    const ownerMdShaped = {
      type: 'user',
      turn_origin: 'owner',
      message: { role: 'user', content: [{ type: 'text', text: '# Heading\n| a | b |\n|---|---|\n| 1 | 2 |\n*not bold*' }] },
    };
    const rows = displayRows([ownerMdShaped], { inspect: true });
    const row = rows.find((r) => r.kind === 'user');
    expect(row).toBeTruthy();
    expect(row!.origin).toBe('owner');
    expect(bubblePaintsMarkdown('user', row!.origin!)).toBe(false);
  });

  it('an agent report with headings/table/code renders as markdown even without shape cues alone', () => {
    const agentReport = {
      type: 'user',
      turn_origin: 'agent',
      message: { role: 'user', content: [{ type: 'text', text: 'plain looking report with no markdown shape at all' }] },
    };
    const rows = displayRows([agentReport], { inspect: true });
    const row = rows.find((r) => r.kind === 'user');
    expect(row).toBeTruthy();
    expect(row!.origin).toBe('agent');
    expect(bubblePaintsMarkdown('user', row!.origin!)).toBe(true);
  });

  it('a plain owner turn with no markdown shape still stays verbatim (control)', () => {
    const owner = {
      type: 'user',
      turn_origin: 'owner',
      message: { role: 'user', content: [{ type: 'text', text: 'just a normal sentence' }] },
    };
    const rows = displayRows([owner], { inspect: true });
    const row = rows.find((r) => r.kind === 'user');
    expect(row!.origin).toBe('owner');
    expect(bubblePaintsMarkdown('user', row!.origin!)).toBe(false);
  });
});
