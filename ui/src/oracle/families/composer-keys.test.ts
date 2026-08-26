// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { decideSend } from '../../composer/sendQueue';
import { classifyEnterAction } from '../../keys/composerEnter';
import { shouldFocusComposer } from '../../keys/composerFocus';
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

  itOracle('T547', 'Tab never advances to theme/send/voice/jump/resize when sidebar composer is hidden', () => {
    const stay = planComposerTabCycle({ key: 'Tab' }, { active: 'main', sidebarVisible: false });
    expect(stay.preventDefault).toBe(true);
    expect(stay.target).toBe('main');
    expect(stay.reason).toBe('stay-main');
  });

  itOracle('T113', 'Enter queues a follow-up instead of interrupting the in-flight turn', () => {
    expect(decideSend({ busy: true, text: 'follow up' })).toEqual({
      action: 'enqueue',
      text: 'follow up',
      reason: 'busy',
    });
    expect(classifyEnterAction('Enter', {}, { composerEmpty: false })).toBe('send');
  });

  itOracle('T127', 'dedicated hotkey always focuses the main composer', () => {
    expect(shouldFocusComposer('/', {}, { tagName: 'DIV' })).toBe(true);
    expect(shouldFocusComposer('/', {}, { tagName: 'TEXTAREA' })).toBe(false);
  });

  itOracle('T132', 'Ctrl+Enter sends now; Alt+Enter empty is queue/noop, not a newline', () => {
    expect(classifyEnterAction('Enter', { ctrlKey: true })).toBe('interrupt');
    expect(classifyEnterAction('Enter', { altKey: true }, { composerEmpty: true, queueLen: 0 })).toBe('noop');
    expect(classifyEnterAction('Enter', { shiftKey: true })).toBe('newline');
  });

  itOracle('T241', 'Alt+Enter force-sends draft if non-empty, else the send-queue head', () => {
    expect(classifyEnterAction('Enter', { altKey: true }, { composerEmpty: false })).toBe('force_send');
    expect(classifyEnterAction('Enter', { altKey: true }, { composerEmpty: true, queueLen: 2 })).toBe(
      'send_queue_now',
    );
  });

  itOracle.skip(
    'T235',
    'Alt+Enter empty/seed-only pops last owner message into the draft',
    'React Enter policy is T241 force_send/queue, not pop_last',
  );
  itOracle.skip('T126', 'Home and End move the caret to start and end of the composer field', 'journey is the arbiter (J24)');
  itOracle.skip('T307', 'Home/End are field edges; Cmd/Ctrl+Left/Right stay word chords', 'journey is the arbiter (J24 Home/End)');
});
