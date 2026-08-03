// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const IT = require('./instant_tip.js');

let failed = 0;
function test(name, fn) {
  try {
    fn();
    console.log('ok  -', name);
  } catch (e) {
    failed++;
    console.error('FAIL-', name);
    console.error('    ', e && e.stack ? e.stack.split('\n').slice(0, 5).join('\n     ') : e);
  }
}

function mockEl(tag) {
  const listeners = {};
  const el = {
    tagName: String(tag || 'div').toUpperCase(),
    className: '',
    textContent: '',
    title: 'SHOULD_BE_CLEARED',
    style: {},
    children: [],
    attrs: {},
    offsetWidth: 200,
    offsetHeight: 100,
    classList: {
      _s: new Set(),
      add(c) {
        this._s.add(c);
        el.className = Array.from(this._s).join(' ');
      },
      remove(c) {
        this._s.delete(c);
        el.className = Array.from(this._s).join(' ');
      },
      contains(c) {
        return this._s.has(c);
      },
    },
    ownerDocument: null,
    getBoundingClientRect() {
      return { left: 10, top: 40, right: 50, bottom: 54, width: 40, height: 14 };
    },
    appendChild(c) {
      this.children.push(c);
      c.parentNode = this;
      return c;
    },
    removeChild(c) {
      const i = this.children.indexOf(c);
      if (i >= 0) this.children.splice(i, 1);
      if (c) c.parentNode = null;
      return c;
    },
    setAttribute(k, v) {
      this.attrs[k] = String(v);
    },
    removeAttribute(k) {
      delete this.attrs[k];
      if (k === 'title') this.title = '';
    },
    addEventListener(type, fn) {
      if (!listeners[type]) listeners[type] = [];
      listeners[type].push(fn);
    },
    dispatch(type, extra) {
      const ev = Object.assign({ type: type }, extra || {});
      (listeners[type] || []).forEach(function (fn) { fn(ev); });
    },
    _listeners: listeners,
  };
  Object.defineProperty(el, 'innerHTML', {
    get() { return this._innerHTML || ''; },
    set(v) {
      this._innerHTML = String(v);
      this.textContent = String(v).replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim();
    },
    configurable: true,
  });
  return el;
}

function mockDoc() {
  const body = mockEl('body');
  const doc = {
    body: body,
    createElement(tag) {
      return mockEl(tag);
    },
  };
  body.ownerDocument = doc;
  return doc;
}

test('SHOW_DELAY_MS is 0 and schedule never uses timeout', function () {
  assert.strictEqual(IT.SHOW_DELAY_MS, 0);
  const sch = IT.showSchedule();
  assert.strictEqual(sch.delayMs, 0);
  assert.strictEqual(sch.usesTimeout, false);
  assert.strictEqual(sch.event, 'pointerenter');
});

test('attach strips title= and sets aria-label', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  host.title = 'Converging';
  const tip = IT.attach(host, 'Converging', { doc: doc, mount: doc.body });
  assert.ok(tip, 'tip created');
  assert.strictEqual(host.title, '');
  assert.strictEqual(host.attrs['aria-label'], 'Converging');
  assert.strictEqual(tip.textContent, 'Converging');
  assert.ok(host.classList.contains(IT.HOST_CLASS));
});

function mockTimers() {
  const pending = [];
  let seq = 0;
  return {
    pending: pending,
    setTimeout(fn, ms) {
      const id = ++seq;
      pending.push({ id: id, fn: fn, ms: ms == null ? 0 : ms });
      return id;
    },
    clearTimeout(id) {
      for (let i = 0; i < pending.length; i++) {
        if (pending[i] && pending[i].id === id) pending[i] = null;
      }
    },
    flush() {
      const batch = pending.splice(0, pending.length).filter(Boolean);
      batch.forEach(function (p) { p.fn(); });
    },
  };
}

test('pointerenter shows tip immediately with no setTimeout (🎯T175)', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const timers = mockTimers();
  const tip = IT.attach(host, '4 targets depend on T10.2', {
    doc: doc, mount: doc.body, timers: timers, hideGraceMs: 0,
  });
  assert.ok(tip);
  assert.strictEqual(IT.isVisible(tip), false);
  // Simulate pointerenter — must show in same turn (no delay queue).
  host.dispatch('pointerenter');
  assert.strictEqual(IT.isVisible(tip), true, 'visible immediately on pointerenter');
  host.dispatch('pointerleave');
  assert.strictEqual(IT.isVisible(tip), false, 'hidden on pointerleave (grace 0)');
});

test('mouseenter also shows (fallback path)', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const timers = mockTimers();
  const tip = IT.attach(host, 'Identified', {
    doc: doc, mount: doc.body, timers: timers, hideGraceMs: 0,
  });
  host.dispatch('mouseenter');
  assert.strictEqual(IT.isVisible(tip), true);
  host.dispatch('mouseleave');
  assert.strictEqual(IT.isVisible(tip), false);
});

test('empty text → no tip, no attach', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  assert.strictEqual(IT.attach(host, '', { doc: doc }), null);
  assert.strictEqual(IT.attach(host, '   ', { doc: doc }), null);
});

test('instant_tip.js source never setTimeout-delays show', function () {
  const src = fs.readFileSync(path.join(__dirname, 'instant_tip.js'), 'utf8');
  // No setTimeout(show, N) path for the product show handler.
  // 🎯T186 may use setTimeout only for hide grace — never for show.
  assert.ok(!/setTimeout\s*\(\s*show/.test(src), 'must not setTimeout before show');
  assert.ok(/SHOW_DELAY_MS\s*=\s*0/.test(src), 'SHOW_DELAY_MS = 0 in source');
  assert.ok(/HIDE_GRACE_MS\s*=\s*\d+/.test(src), 'HIDE_GRACE_MS present');
  const grace = src.match(/HIDE_GRACE_MS\s*=\s*(\d+)/);
  assert.ok(grace, 'HIDE_GRACE_MS numeric');
  const g = Number(grace[1]);
  // 🎯T186 was 50–150; 🎯T187 widens bridge grace to ~300–500ms.
  assert.ok(g >= 50 && g <= 500, 'HIDE_GRACE_MS in 50–500ms: ' + g);
  // pointerenter is the primary event.
  assert.ok(src.indexOf('pointerenter') >= 0);
  // Sticky: tip listens for enter/leave.
  assert.ok(src.indexOf('onTipEnter') >= 0 || /tip\.addEventListener\s*\(\s*['"]pointerenter/.test(src),
    'tip pointerenter for sticky');
  // 🎯T203: singleton registry on show path.
  assert.ok(src.indexOf('dismissOtherTips') >= 0, 'dismissOtherTips in source');
  assert.ok(src.indexOf('openTips') >= 0 || src.indexOf('claimOpen') >= 0, 'open tip registry');
  assert.ok(src.indexOf('_instantTipForceHide') >= 0, 'forceHide for sticky reset');
});

test('index.html wires InstantTip on frontier cells; no title= for status/fanout/name', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('scripts/instant_tip.js') >= 0, 'script tag');
  assert.ok(html.indexOf('InstantTip.attach') >= 0, 'attach used');
  // Render path must not assign native title on frontier explanation cells.
  // Allow other title= elsewhere in chrome; ban status.title / fan.title / name.title
  // in the frontier row builder region.
  const start = html.indexOf('// 🎯T173: headerless table');
  assert.ok(start >= 0, 'frontier render marker');
  const end = html.indexOf('function loadFrontier', start);
  assert.ok(end > start, 'loadFrontier after render');
  const region = html.slice(start, end);
  assert.ok(region.indexOf('status.title') < 0, 'no status.title');
  assert.ok(region.indexOf('fan.title') < 0, 'no fan.title');
  assert.ok(region.indexOf('name.title') < 0, 'no name.title');
  // Custom tip path for the three explanation surfaces.
  assert.ok(/InstantTip\.attach\(\s*status/.test(region) || region.indexOf('InstantTip.attach(status') >= 0,
    'status InstantTip');
  assert.ok(region.indexOf('InstantTip.attach(fan') >= 0 || /InstantTip\.attach\(\s*fan/.test(region),
    'fan InstantTip');
  assert.ok(region.indexOf('InstantTip.attach(name') >= 0 || /InstantTip\.attach\(\s*name/.test(region),
    'name InstantTip');
  // CSS for tip chrome present.
  assert.ok(/\.instant-tip\s*\{/.test(html), 'instant-tip CSS');
});

// 🎯T181: pure left-of-pointer placement — prefer left, flip near left edge, clamp.
test('placeLeftOfPointerRect prefers left of cursor; flips when clipped (🎯T181)', function () {
  // Room on left: tip fully left of pointer, vertically centered.
  const roomy = IT.placeLeftOfPointerRect({
    pointerX: 400,
    pointerY: 300,
    tipW: 200,
    tipH: 100,
    viewW: 1000,
    viewH: 800,
    gap: 12,
    pad: 4,
  });
  assert.strictEqual(roomy.side, 'left');
  assert.strictEqual(roomy.left, 400 - 12 - 200);
  assert.strictEqual(roomy.top, 300 - 50);

  // Near left edge: flip to right of pointer.
  const edge = IT.placeLeftOfPointerRect({
    pointerX: 40,
    pointerY: 200,
    tipW: 200,
    tipH: 80,
    viewW: 800,
    viewH: 600,
    gap: 12,
    pad: 4,
  });
  assert.strictEqual(edge.side, 'right');
  assert.strictEqual(edge.left, 40 + 12);
  assert.strictEqual(edge.top, 200 - 40);

  // Vertical clamp near top.
  const topClamp = IT.placeLeftOfPointerRect({
    pointerX: 500,
    pointerY: 10,
    tipW: 100,
    tipH: 120,
    viewW: 800,
    viewH: 600,
    gap: 12,
    pad: 4,
  });
  assert.ok(topClamp.top >= 4, 'clamped top: ' + topClamp.top);

  // Vertical clamp near bottom.
  const botClamp = IT.placeLeftOfPointerRect({
    pointerX: 500,
    pointerY: 590,
    tipW: 100,
    tipH: 120,
    viewW: 800,
    viewH: 600,
    gap: 12,
    pad: 4,
  });
  assert.ok(botClamp.top + 120 <= 600 - 4 + 1, 'clamped bottom: ' + botClamp.top);
});

test('attach html + left-of-pointer shows immediately with card class (🎯T181)', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const body = '<p><strong>🎯T181</strong> — Card</p><ul><li>Acceptance A</li></ul>';
  const tip = IT.attach(host, body, {
    doc: doc,
    mount: doc.body,
    html: true,
    ariaLabel: '🎯T181 Card. Acceptance A',
    placement: IT.PLACE_LEFT_OF_POINTER,
    className: IT.CARD_CLASS,
    // No table in mock doc — skip clamp so left-of-pointer math is pure.
    clampSelectors: [],
  });
  assert.ok(tip);
  assert.ok(tip.className.indexOf(IT.CARD_CLASS) >= 0, 'card class on tip');
  assert.ok(tip.innerHTML.indexOf('Acceptance A') >= 0, 'html body has acceptance');
  assert.strictEqual(host.attrs['aria-label'].indexOf('Acceptance A') >= 0, true);
  host.dispatch('pointerenter', { clientX: 400, clientY: 250 });
  assert.strictEqual(IT.isVisible(tip), true, '0ms show');
  // Position applied (left of pointer ≈ 400 - 12 - 200).
  assert.ok(tip.style.left, 'left set');
  assert.ok(tip.style.top, 'top set');
  const leftPx = parseInt(tip.style.left, 10);
  assert.ok(leftPx < 400, 'tip placed left of pointer x=400, got ' + leftPx);
  // 🎯T186: tip receives pointer events while shown.
  assert.strictEqual(tip.style.pointerEvents, 'auto', 'pointer-events auto while shown');
});

// 🎯T186: sticky — enter tip after leave host cancels hide.
test('sticky: enter tip after leave host keeps tip open (🎯T186)', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const timers = mockTimers();
  const tip = IT.attach(host, 'Scrollable card body', {
    doc: doc,
    mount: doc.body,
    sticky: true,
    hideGraceMs: 100,
    timers: timers,
    placement: IT.PLACE_LEFT_OF_POINTER,
    className: IT.CARD_CLASS,
    clampSelectors: [],
  });
  assert.ok(tip);
  assert.ok(IT.HIDE_GRACE_MS >= 50 && IT.HIDE_GRACE_MS <= 500, 'grace in range');
  const hs = IT.hideSchedule();
  assert.strictEqual(hs.usesTimeout, true);
  assert.ok(hs.graceMs >= 50 && hs.graceMs <= 500);

  host.dispatch('pointerenter', { clientX: 500, clientY: 200 });
  assert.strictEqual(IT.isVisible(tip), true, 'shown 0ms');
  assert.strictEqual(tip.style.pointerEvents, 'auto');

  // Leave host → hide scheduled, still visible.
  host.dispatch('pointerleave');
  assert.strictEqual(IT.isVisible(tip), true, 'still visible during grace');
  assert.ok(timers.pending.filter(Boolean).length >= 1, 'hide timer scheduled');

  // Enter tip within grace → cancel hide.
  tip.dispatch('pointerenter');
  assert.strictEqual(IT.isVisible(tip), true, 'visible after tip enter');
  timers.flush(); // would fire cancelled timer slots only if still pending
  assert.strictEqual(IT.isVisible(tip), true, 'still visible after flush (cancelled)');

  // Leave tip → schedule hide again → flush → hidden.
  tip.dispatch('pointerleave');
  assert.strictEqual(IT.isVisible(tip), true, 'grace after tip leave');
  timers.flush();
  assert.strictEqual(IT.isVisible(tip), false, 'hidden after leave both + grace');
});

test('sticky: leave host without entering tip hides after grace (🎯T186)', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const timers = mockTimers();
  const tip = IT.attach(host, 'Fanout list', {
    doc: doc, mount: doc.body, sticky: true, hideGraceMs: 80, timers: timers,
  });
  host.dispatch('pointerenter');
  assert.strictEqual(IT.isVisible(tip), true);
  host.dispatch('pointerleave');
  assert.strictEqual(IT.isVisible(tip), true, 'grace');
  assert.ok(timers.pending.some(function (p) { return p && p.ms === 80; }), 'grace ms');
  timers.flush();
  assert.strictEqual(IT.isVisible(tip), false, 'hidden after grace without tip enter');
});

// 🎯T186: clamp card right edge to frontier table left.
test('placeLeftOfPointerRect clamps right edge to maxRight (🎯T186)', function () {
  // Wide tip left of pointer would extend past table at x=500.
  const pos = IT.placeLeftOfPointerRect({
    pointerX: 520,
    pointerY: 200,
    tipW: 200,
    tipH: 100,
    viewW: 1000,
    viewH: 800,
    gap: 12,
    pad: 4,
    maxRight: 500, // table left − gap
  });
  assert.strictEqual(pos.side, 'left');
  assert.ok(pos.left + 200 <= 500 + 0.5, 'right edge ≤ maxRight: left=' + pos.left);
  assert.strictEqual(pos.left, 300); // 500 - 200

  // Prefer left of pointer when it already fits under clamp.
  const roomy = IT.placeLeftOfPointerRect({
    pointerX: 400,
    pointerY: 200,
    tipW: 100,
    tipH: 80,
    viewW: 1000,
    viewH: 800,
    gap: 12,
    pad: 4,
    maxRight: 450,
  });
  assert.strictEqual(roomy.left, 400 - 12 - 100);
  assert.ok(roomy.left + 100 <= 450);

  // Not enough room: shrink maxWidth; do not flip over table.
  const tight = IT.placeLeftOfPointerRect({
    pointerX: 80,
    pointerY: 200,
    tipW: 300,
    tipH: 100,
    viewW: 1000,
    viewH: 800,
    gap: 12,
    pad: 4,
    maxRight: 100,
  });
  assert.strictEqual(tight.side, 'left', 'no flip over table when maxRight set');
  assert.ok(tight.maxWidth != null && tight.maxWidth <= 96, 'shrunk: ' + tight.maxWidth);
  assert.ok(tight.left + (tight.maxWidth || 0) <= 100 + 0.5, 'clamped after shrink');
});

test('resolveClampRight from clampRect / maxRight (🎯T186)', function () {
  assert.strictEqual(IT.resolveClampRight({ maxRight: 420 }), 420);
  assert.strictEqual(
    IT.resolveClampRight({ clampRect: { left: 600 }, clampGap: 8 }),
    592
  );
  assert.strictEqual(IT.DEFAULT_CLAMP_GAP, 8);
  assert.ok(IT.DEFAULT_CLAMP_SELECTORS.indexOf('#frontier-table') >= 0);
  assert.ok(IT.DEFAULT_CLAMP_SELECTORS.indexOf('#frontier-body') >= 0);
});

test('attach left-of-pointer with maxRight clamps card (🎯T186)', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const tip = IT.attach(host, '<p>Card</p>', {
    doc: doc,
    mount: doc.body,
    html: true,
    placement: IT.PLACE_LEFT_OF_POINTER,
    className: IT.CARD_CLASS,
    maxRight: 350,
    // tip mock width 200
  });
  tip.offsetWidth = 200;
  tip.offsetHeight = 120;
  host.dispatch('pointerenter', { clientX: 480, clientY: 300 });
  assert.strictEqual(IT.isVisible(tip), true);
  const left = parseInt(tip.style.left, 10);
  assert.ok(left + 200 <= 350 + 0.5, 'card right ≤ maxRight: left=' + left);
});

test('index.html sticky + clamp wiring (🎯T186)', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(/\.instant-tip\.instant-tip-show\s*\{[^}]*pointer-events:\s*auto/.test(html)
    || /instant-tip-show[\s\S]*?pointer-events:\s*auto/.test(html),
    'show class enables pointer-events');
  const start = html.indexOf('// 🎯T173: headerless table');
  assert.ok(start >= 0);
  const end = html.indexOf('function loadFrontier', start);
  const region = html.slice(start, end > start ? end : start + 9000);
  assert.ok(region.indexOf('sticky') >= 0 || region.indexOf('T186') >= 0, 'sticky/T186 in render');
  assert.ok(region.indexOf('frontier-table') >= 0 || region.indexOf('clampSelectors') >= 0,
    'clamp to frontier table');
  assert.ok(region.indexOf('clampSelectors') >= 0, 'clampSelectors wired');
});

// 🎯T203: product-wide singleton — show second tip hides first.
test('singleton: attach/show second tip hides first (🎯T203)', function () {
  const doc = mockDoc();
  const host1 = mockEl('td');
  const host2 = mockEl('td');
  host1.ownerDocument = doc;
  host2.ownerDocument = doc;
  const tip1 = IT.attach(host1, '<p>Card A — T100</p>', {
    doc: doc, mount: doc.body, html: true, hideGraceMs: 0,
    sticky: true, className: IT.CARD_CLASS, placement: IT.PLACE_LEFT_OF_POINTER,
    clampSelectors: [],
  });
  const tip2 = IT.attach(host2, '<p>Card B — T200</p>', {
    doc: doc, mount: doc.body, html: true, hideGraceMs: 0,
    sticky: true, className: IT.CARD_CLASS, placement: IT.PLACE_LEFT_OF_POINTER,
    clampSelectors: [],
  });
  assert.ok(tip1 && tip2);
  assert.strictEqual(typeof tip1._instantTipForceHide, 'function', 'forceHide wired');
  assert.strictEqual(typeof tip2._instantTipForceHide, 'function');

  host1.dispatch('pointerenter', { clientX: 400, clientY: 200 });
  assert.strictEqual(IT.isVisible(tip1), true, 'first shown');
  assert.strictEqual(IT.openTipsCount(), 1, 'registry has 1');

  host2.dispatch('pointerenter', { clientX: 420, clientY: 220 });
  assert.strictEqual(IT.isVisible(tip2), true, 'second shown');
  assert.strictEqual(IT.isVisible(tip1), false, 'first hidden by singleton');
  assert.strictEqual(IT.openTipsCount(), 1, 'registry still 1');
  assert.strictEqual(IT.getOpenTips()[0], tip2, 'open tip is second');

  // Third: plain text tip also participates in product-wide singleton.
  const host3 = mockEl('td');
  host3.ownerDocument = doc;
  const tip3 = IT.attach(host3, 'Fanout list', {
    doc: doc, mount: doc.body, hideGraceMs: 0, sticky: true,
  });
  host3.dispatch('pointerenter');
  assert.strictEqual(IT.isVisible(tip3), true);
  assert.strictEqual(IT.isVisible(tip2), false, 'card dismissed by text tip');
  assert.strictEqual(IT.openTipsCount(), 1);
});

test('singleton: showTip path dismisses peers without attach sticky (🎯T203)', function () {
  const doc = mockDoc();
  const a = mockEl('div');
  const b = mockEl('div');
  a.className = IT.TIP_CLASS;
  b.className = IT.TIP_CLASS;
  a.style = {};
  b.style = {};
  // Standalone tips (no attach forceHide) still hide via hideTip.
  IT.showTip(a, null, {});
  assert.strictEqual(IT.isVisible(a), true);
  assert.strictEqual(IT.openTipsCount(), 1);
  IT.showTip(b, null, {});
  assert.strictEqual(IT.isVisible(b), true);
  assert.strictEqual(IT.isVisible(a), false, 'peer hidden on showTip');
  assert.strictEqual(IT.openTipsCount(), 1);
  IT.hideTip(b);
  assert.strictEqual(IT.openTipsCount(), 0);
});

// ─── 🎯T187: no auto-timeout while over tip; gap grace only ─────────────────

test('T187 pure: shouldRunScheduledHide never true while overTip', function () {
  assert.strictEqual(IT.shouldRunScheduledHide({ overTip: true, overHost: false }), false);
  assert.strictEqual(IT.shouldRunScheduledHide({ overTip: true, overHost: true }), false);
  assert.strictEqual(IT.shouldRunScheduledHide({ overTip: false, overHost: true }), false);
  assert.strictEqual(IT.shouldRunScheduledHide({ overTip: false, overHost: false }), true);
  assert.strictEqual(
    IT.shouldScheduleHideOnHostLeave({ overTip: true, tipEngaged: true }),
    false,
    'leave-host alone while over tip must not schedule'
  );
  assert.strictEqual(
    IT.shouldScheduleHideOnHostLeave({ overTip: false, tipEngaged: true }),
    true,
    'walk-away after tip→host still schedules'
  );
  assert.strictEqual(
    IT.shouldScheduleHideOnTipLeave({ overHost: true, overTip: false }),
    false
  );
  assert.strictEqual(
    IT.shouldScheduleHideOnTipLeave({ overHost: false, overTip: false }),
    true
  );
});

test('T187 hideSchedule: gap-only grace, no setInterval, neverWhileOverTip', function () {
  const hs = IT.hideSchedule();
  assert.strictEqual(hs.usesInterval, false, 'no setInterval auto-hide');
  assert.strictEqual(hs.gapOnly, true);
  assert.strictEqual(hs.neverWhileOverTip, true);
  assert.strictEqual(hs.usesTimeout, true);
  assert.ok(hs.graceMs >= 300 && hs.graceMs <= 500, 'product grace 300–500ms: ' + hs.graceMs);
  assert.strictEqual(IT.HIDE_GRACE_MS, hs.graceMs);
});

test('T187 source has no setInterval auto-hide path', function () {
  const src = fs.readFileSync(path.join(__dirname, 'instant_tip.js'), 'utf8');
  assert.ok(!/setInterval\s*\(/.test(src), 'no setInterval in instant_tip.js');
  assert.ok(src.indexOf('shouldRunScheduledHide') >= 0);
  assert.ok(src.indexOf('neverWhileOverTip') >= 0 || src.indexOf('overTip') >= 0);
  assert.ok(/HIDE_GRACE_MS\s*=\s*([3-5]\d{2})/.test(src), 'HIDE_GRACE_MS in 300–500');
});

test('T187: overTip true → scheduled hide does not run', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const timers = mockTimers();
  const tip = IT.attach(host, 'Sticky card — no idle timeout', {
    doc: doc,
    mount: doc.body,
    sticky: true,
    hideGraceMs: 50,
    timers: timers,
    placement: IT.PLACE_LEFT_OF_POINTER,
    className: IT.CARD_CLASS,
    clampSelectors: [],
  });
  host.dispatch('pointerenter', { clientX: 500, clientY: 200 });
  assert.strictEqual(IT.isVisible(tip), true);

  // Leave host then enter tip (bridge).
  host.dispatch('pointerleave');
  assert.ok(timers.pending.filter(Boolean).length >= 1, 'gap grace armed');
  tip.dispatch('pointerenter');
  assert.strictEqual(tip._instantTipHoverState().overTip, true);
  assert.strictEqual(tip._instantTipHoverState().tipEngaged, true);

  // While over tip, force-arm a hide timer as if something re-scheduled — callback must no-op.
  // Entering tip should have cancelled; flush any cancelled slots then verify still open.
  timers.flush();
  assert.strictEqual(IT.isVisible(tip), true, 'still open after grace flush while overTip');

  // leave-host alone while over tip must not hide (re-dispatch host leave).
  host.dispatch('pointerleave');
  assert.strictEqual(IT.isVisible(tip), true, 'leave-host alone while over tip keeps open');
  assert.strictEqual(
    timers.pending.filter(Boolean).length,
    0,
    'no hide timer while overTip'
  );
  timers.flush();
  assert.strictEqual(IT.isVisible(tip), true, 'still open after idle flush over tip');

  // Wheel over tip must not dismiss.
  tip.dispatch('wheel', { deltaY: 40 });
  assert.strictEqual(IT.isVisible(tip), true);
  assert.strictEqual(tip._instantTipHoverState().overTip, true);
  timers.flush();
  assert.strictEqual(IT.isVisible(tip), true, 'wheel does not auto-hide');

  // Real dismiss: leave tip (not over host).
  tip.dispatch('pointerleave');
  assert.strictEqual(IT.isVisible(tip), true, 'grace after tip leave');
  timers.flush();
  assert.strictEqual(IT.isVisible(tip), false, 'hidden after leave tip + grace');
});

test('T187: after tip entered, leave-host alone does not hide', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const timers = mockTimers();
  const tip = IT.attach(host, 'Engaged card', {
    doc: doc, mount: doc.body, sticky: true, hideGraceMs: 40, timers: timers,
  });
  host.dispatch('pointerenter');
  tip.dispatch('pointerenter');
  assert.strictEqual(tip._instantTipHoverState().tipEngaged, true);
  assert.strictEqual(tip._instantTipHoverState().overTip, true);

  // Host leave while still over tip — must not arm hide.
  host.dispatch('pointerleave');
  assert.strictEqual(IT.isVisible(tip), true);
  assert.ok(
    timers.pending.filter(Boolean).length === 0,
    'no grace timer when overTip on host leave'
  );

  // Nested leave (relatedTarget still inside tip) ignored.
  const child = mockEl('div');
  tip.appendChild(child);
  tip.contains = function (n) { return n === tip || n === child || (tip.children && tip.children.indexOf(n) >= 0); };
  tip.dispatch('pointerleave', { relatedTarget: child });
  assert.strictEqual(IT.isVisible(tip), true, 'nested relatedTarget stay open');
  assert.strictEqual(tip._instantTipHoverState().overTip, true);
});

test('T187: leave tip after engagement hides; walk-away via host after tip also hides', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const timers = mockTimers();
  const tip = IT.attach(host, 'Walk away', {
    doc: doc, mount: doc.body, sticky: true, hideGraceMs: 30, timers: timers,
  });
  host.contains = function (n) { return n === host; };
  tip.contains = function (n) { return n === tip; };

  host.dispatch('pointerenter');
  tip.dispatch('pointerenter');
  // Return to host from tip (relatedTarget = host).
  tip.dispatch('pointerleave', { relatedTarget: host });
  assert.strictEqual(IT.isVisible(tip), true, 'tip→host stays open');
  assert.strictEqual(tip._instantTipHoverState().overHost, true);
  assert.strictEqual(tip._instantTipHoverState().overTip, false);

  // Leave host to void — must schedule hide (not stuck).
  host.dispatch('pointerleave');
  assert.strictEqual(IT.isVisible(tip), true, 'grace');
  assert.ok(timers.pending.filter(Boolean).length >= 1, 'walk-away schedules hide');
  timers.flush();
  assert.strictEqual(IT.isVisible(tip), false, 'hidden after tip→host→leave host');
});

// ─── 🎯T230: no wall-clock dismiss while over card; re-render must not kill ─

test('T230 pure: isHoverLatchedState only while overTip/overHost and visible', function () {
  assert.strictEqual(IT.isHoverLatchedState({ overTip: true, overHost: false }, true), true);
  assert.strictEqual(IT.isHoverLatchedState({ overTip: false, overHost: true }, true), true);
  assert.strictEqual(IT.isHoverLatchedState({ overTip: true, overHost: true }, true), true);
  assert.strictEqual(
    IT.isHoverLatchedState({ overTip: false, overHost: false }, true),
    false,
    'gap grace (left both, still visible) is not latched'
  );
  assert.strictEqual(IT.isHoverLatchedState({ overTip: true }, false), false, 'hidden not latched');
  assert.strictEqual(IT.isHoverLatchedState(null, true), false);
});

test('T230: still pointer over tip — no hide timer; leave schedules hide', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const timers = mockTimers();
  const tip = IT.attach(host, '<p>Frontier card body</p>', {
    doc: doc,
    mount: doc.body,
    html: true,
    sticky: true,
    hideGraceMs: 100,
    timers: timers,
    placement: IT.PLACE_LEFT_OF_POINTER,
    className: IT.CARD_CLASS,
    clampSelectors: [],
  });
  host.dispatch('pointerenter', { clientX: 400, clientY: 200 });
  tip.dispatch('pointerenter');
  assert.strictEqual(IT.isVisible(tip), true);
  assert.strictEqual(IT.isHoverLatched(tip), true, 'latched over tip');
  assert.strictEqual(IT.anyHoverLatched(), true);

  // Still pointer: flush many timer slots — must stay open (no idle auto-hide).
  timers.flush();
  timers.flush();
  assert.strictEqual(IT.isVisible(tip), true, 'still open after idle flushes');
  assert.strictEqual(
    timers.pending.filter(Boolean).length,
    0,
    'no hide timer armed while overTip'
  );
  assert.strictEqual(tip._instantTipHoverState().overTip, true);

  // Leave host while still on card — must stay open (no timer).
  host.dispatch('pointerleave');
  assert.strictEqual(IT.isVisible(tip), true, 'leave-host alone while over tip keeps open');
  assert.strictEqual(IT.isHoverLatched(tip), true, 'still latched over tip');
  assert.strictEqual(timers.pending.filter(Boolean).length, 0, 'no hide while overTip');

  // Leave tip (not over host) → grace schedules hide.
  tip.dispatch('pointerleave');
  assert.strictEqual(tip._instantTipHoverState().overTip, false);
  assert.strictEqual(tip._instantTipHoverState().overHost, false);
  assert.strictEqual(IT.isHoverLatched(tip), false, 'not latched after leave tip+host');
  assert.ok(timers.pending.filter(Boolean).length >= 1, 'leave schedules hide');
  timers.flush();
  assert.strictEqual(IT.isVisible(tip), false, 'hidden after leave + grace');
  assert.strictEqual(IT.anyHoverLatched(), false);
});

test('T230: discardDetachedTips preserves latched tip, removes others', function () {
  const doc = mockDoc();
  const body = doc.body;
  // querySelectorAll mock for discardDetachedTips
  const tips = [];
  doc.querySelectorAll = function (sel) {
    if (String(sel).indexOf('instant-tip') >= 0) return tips.slice();
    return [];
  };

  const hostA = mockEl('td');
  hostA.ownerDocument = doc;
  const hostB = mockEl('td');
  hostB.ownerDocument = doc;
  const tipA = IT.attach(hostA, 'Card A latched', {
    doc: doc, mount: body, sticky: true, hideGraceMs: 0,
  });
  const tipB = IT.attach(hostB, 'Card B closed', {
    doc: doc, mount: body, sticky: true, hideGraceMs: 0,
  });
  tips.push(tipA, tipB);

  hostA.dispatch('pointerenter');
  tipA.dispatch('pointerenter');
  assert.strictEqual(IT.isVisible(tipA), true);
  assert.strictEqual(IT.isHoverLatched(tipA), true);
  // tipB never shown — not latched
  assert.strictEqual(IT.isHoverLatched(tipB), false);

  const r = IT.discardDetachedTips(doc);
  assert.strictEqual(r.preserved, 1, 'latched preserved');
  assert.strictEqual(r.removed, 1, 'closed removed');
  assert.strictEqual(IT.isVisible(tipA), true, 'latched still visible');
  assert.ok(tipA.parentNode === body || body.children.indexOf(tipA) >= 0, 'latched still mounted');
  assert.ok(!tipB.parentNode || body.children.indexOf(tipB) < 0, 'closed unmounted');
});

test('T230: anyHoverLatched false after leave both (gap grace still visible)', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const timers = mockTimers();
  const tip = IT.attach(host, 'Grace gap', {
    doc: doc, mount: doc.body, sticky: true, hideGraceMs: 80, timers: timers,
  });
  host.dispatch('pointerenter');
  assert.strictEqual(IT.anyHoverLatched(), true, 'over host latched');
  host.dispatch('pointerleave');
  // During gap grace: still visible, both flags false → not latched (remount OK).
  assert.strictEqual(IT.isVisible(tip), true, 'visible during grace');
  assert.strictEqual(tip._instantTipHoverState().overHost, false);
  assert.strictEqual(tip._instantTipHoverState().overTip, false);
  assert.strictEqual(IT.isHoverLatched(tip), false, 'gap not latched');
  assert.strictEqual(IT.anyHoverLatched(), false);
  timers.flush();
  assert.strictEqual(IT.isVisible(tip), false);
});

test('T230 index.html skips re-render while anyHoverLatched; discardDetachedTips path', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const start = html.indexOf('function renderFrontierTable');
  assert.ok(start >= 0, 'renderFrontierTable present');
  const end = html.indexOf('function loadFrontier', start);
  const region = html.slice(start, end > start ? end : start + 4000);
  assert.ok(region.indexOf('anyHoverLatched') >= 0, 'anyHoverLatched gate');
  assert.ok(region.indexOf('T230') >= 0, 'T230 comment');
  assert.ok(region.indexOf('discardDetachedTips') >= 0, 'discardDetachedTips cleanup');
  // Must not blindly removeChild all tips without the latch gate.
  assert.ok(
    /anyHoverLatched[\s\S]*return;/.test(region) || region.indexOf('anyHoverLatched()') >= 0,
    'early return when latched'
  );
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('All instant_tip tests passed');

