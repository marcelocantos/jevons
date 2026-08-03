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
  assert.ok(g >= 50 && g <= 150, 'HIDE_GRACE_MS in 50–150ms: ' + g);
  // pointerenter is the primary event.
  assert.ok(src.indexOf('pointerenter') >= 0);
  // Sticky: tip listens for enter/leave.
  assert.ok(src.indexOf('onTipEnter') >= 0 || /tip\.addEventListener\s*\(\s*['"]pointerenter/.test(src),
    'tip pointerenter for sticky');
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
  assert.ok(IT.HIDE_GRACE_MS >= 50 && IT.HIDE_GRACE_MS <= 150, 'grace in range');
  const hs = IT.hideSchedule();
  assert.strictEqual(hs.usesTimeout, true);
  assert.ok(hs.graceMs >= 50 && hs.graceMs <= 150);

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

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('All instant_tip tests passed');

