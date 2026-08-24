// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { decideSend, shouldEnqueue, shouldInterrupt } from '../../composer/sendQueue';
import { EMPTY_SEED, isEffectivelyEmpty, seedPrefixLen, stripSeed } from '../../composer/wispr';
import { caretAfterEnd, caretAfterHome, selectionAfterHomeEnd, shouldAllowJumpToBottom } from '../../keys/composerCaret';
import { classifyEnterAction, isEnterKey } from '../../keys/composerEnter';
import { isFocusComposerHotkey, shouldFocusComposer, tryFocusComposer } from '../../keys/composerFocus';
import { COMPOSER_TAB_CHROME, planComposerTabCycle } from '../../keys/composerTab';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

function seedOpts(value: string) {
  return {
    seedPrefixLen: seedPrefixLen(value),
    effectiveLength: stripSeed(value).length,
    isSeedOnly: isEffectivelyEmpty(value) && value.length > 0,
  };
}

describeOracle(family('composer-keys'), () => {
  itOracle('T366', 'Tab cycles main ↔ sidebar when the sidebar composer is visible', () => {
    const toSide = planComposerTabCycle({ key: 'Tab' }, { active: 'main', sidebarVisible: true });
    expect(toSide).toEqual({ target: 'sidebar', preventDefault: true, reason: 'to-sidebar' });
    const toMain = planComposerTabCycle({ key: 'Tab' }, { active: 'sidebar', sidebarVisible: true });
    expect(toMain.target).toBe('main');
    expect(toMain.preventDefault).toBe(true);
    const again = planComposerTabCycle({ key: 'Tab' }, { active: 'main', sidebarVisible: true });
    expect(again.target).toBe('sidebar');
  });

  itOracle('T153', 'Tab does not walk chrome when focus is not in a composer', () => {
    const p = planComposerTabCycle({ key: 'Tab' }, { active: 'other', sidebarVisible: true });
    expect(p.preventDefault).toBe(false);
    expect(p.target).toBeNull();
    expect(COMPOSER_TAB_CHROME.includes(p.target as (typeof COMPOSER_TAB_CHROME)[number])).toBe(false);
  });

  itOracle('T547', 'Tab never advances to theme/send/voice/jump/resize when sidebar composer is hidden', () => {
    const stay = planComposerTabCycle({ key: 'Tab' }, { active: 'main', sidebarVisible: false });
    expect(stay.preventDefault).toBe(true);
    expect(stay.target).toBe('main');
    expect(stay.reason).toBe('stay-main');
    expect(COMPOSER_TAB_CHROME).not.toContain(stay.target);
    const shift = planComposerTabCycle({ key: 'Tab', shiftKey: true }, { active: 'main', sidebarVisible: false });
    expect(shift.preventDefault).toBe(true);
    expect(shift.target).toBe('main');
    const fromSide = planComposerTabCycle({ key: 'Tab' }, { active: 'sidebar', sidebarVisible: false });
    expect(fromSide.target).toBe('main');
    expect(fromSide.preventDefault).toBe(true);
  });

  itOracle('T113', 'Enter queues a follow-up instead of interrupting the in-flight turn', () => {
    expect(classifyEnterAction('Enter', {}, { composerEmpty: false })).toBe('send');
    expect(shouldEnqueue(true, false)).toBe(true);
    expect(shouldInterrupt(true, false)).toBe(false);
    const queued = decideSend({ busy: true, interrupt: false, text: 'follow up' });
    expect(queued.action).toBe('enqueue');
    expect(queued.action === 'enqueue' && queued.text).toBe('follow up');
    const now = decideSend({ busy: true, interrupt: true, text: 'interject now' });
    expect(now.action).toBe('send');
    expect(now.action === 'send' && now.interrupt).toBe(true);
    expect(decideSend({ busy: false, interrupt: false, text: 'hello' }).action).toBe('send');
    expect(decideSend({ busy: true, interrupt: false, text: '  ' }).action).toBe('noop');
  });

  itOracle('T126', 'Home and End move the caret to start and end of the composer field', () => {
    const value = 'hello world';
    expect(caretAfterHome(value, 5)).toBe(0);
    expect(caretAfterEnd(value, 0)).toBe(value.length);
    expect(selectionAfterHomeEnd('Home', value, 5, 5, {})).toEqual({ start: 0, end: 0 });
    expect(selectionAfterHomeEnd('End', value, 0, 0, {})).toEqual({
      start: value.length,
      end: value.length,
    });
    expect(selectionAfterHomeEnd('Home', 'abcdef', 3, 3, { shiftKey: true })).toEqual({
      start: 0,
      end: 3,
    });
    expect(selectionAfterHomeEnd('End', 'ab', 1, 1, { altKey: true })).toBeNull();
    expect(shouldAllowJumpToBottom('End', {}, { composerFocused: true })).toBe(false);
    expect(shouldAllowJumpToBottom('End', {}, { composerFocused: false })).toBe(true);
  });

  itOracle('T127', 'dedicated hotkey always focuses the main composer', () => {
    expect(isFocusComposerHotkey('/', {})).toBe(true);
    expect(isFocusComposerHotkey('Slash', {})).toBe(true);
    expect(isFocusComposerHotkey('/', { metaKey: true })).toBe(false);
    expect(shouldFocusComposer('/', {}, { tagName: 'BUTTON' })).toBe(true);
    expect(shouldFocusComposer('/', {}, { tagName: 'TEXTAREA', type: 'text' })).toBe(false);
    let active: { tagName: string; id: string; focus?: () => void } = {
      tagName: 'BUTTON',
      id: 'agent-inspect-refresh',
    };
    const composer = {
      tagName: 'TEXTAREA',
      id: 'input',
      focus: () => {
        active = composer;
      },
    };
    const r = tryFocusComposer({ key: '/' }, composer, active);
    expect(r.didFocus).toBe(true);
    expect(active).toBe(composer);
    expect(tryFocusComposer({ key: '/' }, composer, composer).didFocus).toBe(false);
    expect(tryFocusComposer({ key: '/' }, null, { tagName: 'BUTTON' }).reason).toBe('no-composer');
  });

  itOracle('T132', 'Ctrl+Enter sends now; Alt+Enter empty never pops last owner', () => {
    expect(classifyEnterAction('Enter', { ctrlKey: true }, { composerEmpty: false })).toBe('interrupt');
    expect(classifyEnterAction('Enter', { ctrlKey: true }, { composerEmpty: true })).toBe('interrupt');
    expect(classifyEnterAction('Enter', { ctrlKey: true, altKey: true }, { composerEmpty: true })).toBe(
      'interrupt',
    );
    expect(classifyEnterAction('Enter', {}, { composerEmpty: false })).toBe('send');
    expect(classifyEnterAction('Enter', { shiftKey: true }, {})).toBe('newline');
    expect(classifyEnterAction('Enter', { altKey: true }, { composerEmpty: true, queueLen: 0 })).not.toBe(
      'send',
    );
    expect(classifyEnterAction('Enter', { altKey: true }, { composerEmpty: true, queueLen: 0 })).not.toBe(
      'interrupt',
    );
    expect(String(classifyEnterAction('Enter', { altKey: true }, { composerEmpty: true, queueLen: 0 }))).not.toBe(
      'pop_last',
    );
  });

  itOracle('T235', 'Alt+Enter empty/seed-only is classified via code Enter — never pop_last', () => {
    expect(isEnterKey('Enter', {})).toBe(true);
    expect(isEnterKey('', { code: 'Enter' })).toBe(true);
    expect(isEnterKey(null, { code: 'NumpadEnter' })).toBe(true);
    expect(
      classifyEnterAction('', { altKey: true, code: 'Enter' }, {
        composerEmpty: true,
        code: 'Enter',
        queueLen: 0,
      }),
    ).toBe('noop');
    expect(
      classifyEnterAction('Dead', { altKey: true, code: 'Enter' }, {
        composerEmpty: isEffectivelyEmpty(EMPTY_SEED),
        code: 'Enter',
        queueLen: 1,
      }),
    ).toBe('send_queue_now');
    expect(
      classifyEnterAction('', { altKey: true, code: 'Enter' }, {
        composerEmpty: false,
        code: 'Enter',
      }),
    ).toBe('force_send');
    expect(
      String(
        classifyEnterAction('Enter', { altKey: true }, {
          composerEmpty: isEffectivelyEmpty(EMPTY_SEED),
          queueLen: 0,
        }),
      ),
    ).not.toBe('pop_last');
  });

  itOracle('T241', 'Alt+Enter force-sends draft if non-empty, else the send-queue head', () => {
    expect(classifyEnterAction('Enter', { altKey: true }, { composerEmpty: false })).toBe('force_send');
    expect(
      classifyEnterAction('Enter', { altKey: true }, {
        composerEmpty: isEffectivelyEmpty('real draft'),
        queueLen: 0,
      }),
    ).toBe('force_send');
    expect(
      classifyEnterAction('Enter', { altKey: true }, {
        composerEmpty: isEffectivelyEmpty(EMPTY_SEED + 'Are we done?'),
        queueLen: 3,
      }),
    ).toBe('force_send');
    expect(
      classifyEnterAction('Enter', { altKey: true }, { composerEmpty: true, queueLen: 1 }),
    ).toBe('send_queue_now');
    expect(
      classifyEnterAction('Enter', { altKey: true }, {
        composerEmpty: isEffectivelyEmpty(EMPTY_SEED),
        queueLen: 2,
      }),
    ).toBe('send_queue_now');
    expect(
      classifyEnterAction('Enter', { altKey: true }, { composerEmpty: true, queueLen: 0 }),
    ).toBe('noop');
    expect(String(classifyEnterAction('Enter', { altKey: true }, { composerEmpty: false }))).not.toBe(
      'pop_last',
    );
  });

  itOracle('T307', 'Home/End are field edges; Cmd/Ctrl+Left/Right stay word chords', () => {
    const value = 'abcdef';
    expect(selectionAfterHomeEnd('ArrowLeft', value, 4, 4, { metaKey: true })).toBeNull();
    expect(selectionAfterHomeEnd('ArrowRight', value, 1, 1, { metaKey: true })).toBeNull();
    expect(selectionAfterHomeEnd('ArrowLeft', value, 4, 4, { ctrlKey: true })).toBeNull();
    expect(selectionAfterHomeEnd('ArrowRight', value, 1, 1, { ctrlKey: true })).toBeNull();
    expect(selectionAfterHomeEnd('ArrowLeft', value, 4, 4, { metaKey: true, shiftKey: true })).toBeNull();
    const multi = 'first line\nsecond line\nthird';
    const midSecond = multi.indexOf('second') + 3;
    expect(selectionAfterHomeEnd('Home', multi, midSecond, midSecond, {}, seedOpts(multi))).toEqual({
      start: 0,
      end: 0,
    });
    expect(selectionAfterHomeEnd('End', multi, midSecond, midSecond, {}, seedOpts(multi))).toEqual({
      start: multi.length,
      end: multi.length,
    });
    const seeded = EMPTY_SEED + multi;
    const seededMid = seeded.indexOf('second') + 3;
    expect(selectionAfterHomeEnd('Home', seeded, seededMid, seededMid, {}, seedOpts(seeded))).toEqual({
      start: EMPTY_SEED.length,
      end: EMPTY_SEED.length,
    });
  });
});
