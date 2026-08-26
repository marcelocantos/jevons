// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { planComposerTabCycle } from './composerTab';

describe('planComposerTabCycle', () => {
  it('Tab from main goes to sidebar when visible', () => {
    const p = planComposerTabCycle({ key: 'Tab' }, { active: 'main', sidebarVisible: true });
    expect(p).toEqual({ target: 'sidebar', preventDefault: true, reason: 'to-sidebar' });
  });

  it('Tab from sidebar goes to main', () => {
    const p = planComposerTabCycle({ key: 'Tab' }, { active: 'sidebar', sidebarVisible: true });
    expect(p.target).toBe('main');
    expect(p.preventDefault).toBe(true);
  });

  it('does not claim Tab when focus is not in a composer', () => {
    const p = planComposerTabCycle({ key: 'Tab' }, { active: 'other', sidebarVisible: true });
    expect(p.preventDefault).toBe(false);
    expect(p.target).toBeNull();
  });

  it('claims Tab and stays on main when sidebar composer is hidden (T549)', () => {
    const p = planComposerTabCycle({ key: 'Tab' }, { active: 'main', sidebarVisible: false });
    expect(p.preventDefault).toBe(true);
    expect(p.target).toBe('main');
    expect(p.reason).toBe('stay-main');
  });
});
