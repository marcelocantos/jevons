// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import '../../composer/ensureLocalStorage';
import { createElement } from 'react';
import { fireEvent, render } from '@testing-library/react';
import { afterEach, expect } from 'vitest';
import { decideSend } from '../../composer/sendQueue';
import { UserRequest } from '../../components/UserRequest';
import { classifyEnterAction } from '../../keys/composerEnter';
import { applyComposerHomeEnd, selectionAfterHomeEnd } from '../../keys/composerCaret';
import { shouldFocusComposer } from '../../keys/composerFocus';
import { isSidebarComposerFocusable, planComposerTabCycle } from '../../keys/composerTab';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { useDrafts } from '../../store/drafts';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

afterEach(() => {
  useDrafts.setState({ drafts: {} });
});

describeOracle(family('composer-keys'), () => {
  itOracle('T366', 'Tab cycles main ↔ sidebar when the sidebar composer is visible', () => {
    const toSide = planComposerTabCycle({ key: 'Tab' }, { active: 'main', sidebarVisible: true });
    expect(toSide).toEqual({ target: 'sidebar', preventDefault: true, reason: 'to-sidebar' });
    const toMain = planComposerTabCycle({ key: 'Tab' }, { active: 'sidebar', sidebarVisible: true });
    expect(toMain.target).toBe('main');
    expect(toMain.preventDefault).toBe(true);
  });

  itOracle('T153', 'Send returns focus to the composer so Tab stays locked on the box', () => {
    const { container } = render(createElement(UserRequest, { name: 'jevons', onSend: () => {} }));
    const box = container.querySelector('#input') as HTMLTextAreaElement;
    const send = container.querySelector('#send') as HTMLButtonElement;
    expect(box).toBeTruthy();
    fireEvent.change(box, { target: { value: 'lock me' } });
    box.focus();
    fireEvent.mouseDown(send);
    fireEvent.click(send);
    expect(document.activeElement).toBe(box);
  });

  itOracle('T571', 'Tab stays on main unless the sidebar Transcript pane is actually on screen', () => {
    const hidden = planComposerTabCycle({ key: 'Tab' }, { active: 'main', sidebarVisible: false });
    expect(hidden).toEqual({ target: 'main', preventDefault: true, reason: 'stay-main' });
    const shown = planComposerTabCycle({ key: 'Tab' }, { active: 'main', sidebarVisible: true });
    expect(shown.target).toBe('sidebar');
    expect(shown.preventDefault).toBe(true);
    document.body.innerHTML =
      '<div id="agent-inspect" class="rhs-tab-pane">' +
      '<div id="agent-inspect-composer" class="visible">' +
      '<textarea data-composer="sidebar"></textarea></div></div>';
    expect(isSidebarComposerFocusable(document)).toBe(false);
    document.getElementById('agent-inspect')!.classList.add('active');
    expect(isSidebarComposerFocusable(document)).toBe(true);
    document.body.innerHTML = '';
    const here = dirname(fileURLToPath(import.meta.url));
    const app = readFileSync(join(here, '../../App.tsx'), 'utf8');
    expect(app).toMatch(/paneActive=\{tab === 'transcript'\}/);
    expect(app).toMatch(/useCockpitKeys\(\)/);
  });

  itOracle('T549', 'Tab never advances to theme/send/voice/jump/resize when sidebar composer is hidden', () => {
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
  itOracle(['T126', 'T149', 'T540.3.1'], 'Home and End move the caret to start and end of the composer field', () => {
    useDrafts.getState().setDraft('jevons', 'alpha bravo charlie');
    const { container } = render(createElement(UserRequest, { name: 'jevons', onSend: () => {} }));
    const box = container.querySelector('#input') as HTMLTextAreaElement;
    expect(box).toBeTruthy();
    box.setSelectionRange(8, 8);
    fireEvent.keyDown(box, { key: 'Home' });
    expect(box.selectionStart).toBe(0);
    expect(box.selectionEnd).toBe(0);
    fireEvent.keyDown(box, { key: 'End' });
    expect(box.selectionStart).toBe(box.value.length);
    expect(box.selectionEnd).toBe(box.value.length);
    box.setSelectionRange(6, 6);
    fireEvent.keyDown(box, { key: 'Home', shiftKey: true });
    expect(box.selectionStart).toBe(0);
    expect(box.selectionEnd).toBe(6);
  });

  itOracle('T307', 'Home/End are field edges; Cmd/Ctrl+Left/Right stay native line/word chords', () => {
    const draft = 'alpha\nbravo charlie';
    expect(selectionAfterHomeEnd('Home', draft, 10, 10, {})).toEqual({ start: 0, end: 0 });
    expect(selectionAfterHomeEnd('End', draft, 1, 1, {})).toEqual({ start: draft.length, end: draft.length });
    expect(selectionAfterHomeEnd('ArrowLeft', draft, 10, 10, { metaKey: true })).toBeNull();
    expect(selectionAfterHomeEnd('ArrowRight', draft, 10, 10, { ctrlKey: true })).toBeNull();
    const el = { value: draft, selectionStart: 10, selectionEnd: 10, setSelectionRange: () => {} };
    expect(applyComposerHomeEnd(el, { key: 'ArrowLeft', metaKey: true, preventDefault: () => {} })).toBe(false);
  });
});
