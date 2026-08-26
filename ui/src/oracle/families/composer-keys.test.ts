// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { planComposerTabCycle } from '../../keys/composerTab';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('composer-keys'), () => {
  itOracle('T366', 'Tab cycles main ↔ sidebar when the sidebar composer is visible', () => {
    const toSide = planComposerTabCycle({ key: 'Tab' }, { active: 'main', sidebarVisible: true });
    expect(toSide).toEqual({ target: 'sidebar', preventDefault: true, reason: 'to-sidebar' });
    const toMain = planComposerTabCycle({ key: 'Tab' }, { active: 'sidebar', sidebarVisible: true });
    expect(toMain.target).toBe('main');
    expect(toMain.preventDefault).toBe(true);
  });

  itOracle('T153', 'Tab does not walk chrome when focus is not in a composer', () => {
    const p = planComposerTabCycle({ key: 'Tab' }, { active: 'other', sidebarVisible: true });
    expect(p.preventDefault).toBe(false);
    expect(p.target).toBeNull();
  });

  itOracle.todo('T547', 'Tab never advances to theme/send/voice/jump/resize when sidebar composer is hidden');
  itOracle.todo('T113', 'Enter queues a follow-up instead of interrupting the in-flight turn');
  itOracle.todo('T126', 'Home and End move the caret to start and end of the composer field');
  itOracle.todo('T127', 'dedicated hotkey always focuses the main composer');
  itOracle.todo('T132', 'Ctrl+Enter sends now; Alt+Enter empty pops last owner message');
  itOracle.todo('T235', 'Alt+Enter empty/seed-only pops last owner message into the draft');
  itOracle.todo('T241', 'Alt+Enter force-sends draft if non-empty, else the send-queue head');
  itOracle.todo('T307', 'Home/End are field edges; Cmd/Ctrl+Left/Right stay word chords');
});
