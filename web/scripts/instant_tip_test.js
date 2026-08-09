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
    console.error('    ', e && e.stack ? e.stack.split('\n').slice(0, 6).join('\n     ') : e);
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
    _rect: { left: 10, top: 40, right: 50, bottom: 54, width: 40, height: 14 },
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
      return Object.assign({}, this._rect);
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
    addEventListener() {},
    removeEventListener() {},
  };
  body.ownerDocument = doc;
  return doc;
}

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

// ─── Core show path ───────────────────────────────────────────────────────

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

test('pointerenter shows tip immediately with no setTimeout (🎯T175)', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const timers = mockTimers();
  const tip = IT.attach(host, '4 targets depend on T10.2', {
    doc: doc, mount: doc.body, timers: timers, hitGroup: true,
  });
  assert.ok(tip);
  assert.strictEqual(IT.isVisible(tip), false);
  host.dispatch('pointerenter', { clientX: 20, clientY: 45 });
  assert.strictEqual(IT.isVisible(tip), true, 'visible immediately on pointerenter');
  // Leave outside hit rect → immediate hide (grace 0).
  host.dispatch('pointerleave', { clientX: 900, clientY: 900 });
  assert.strictEqual(IT.isVisible(tip), false, 'hidden on leave outside hit rect');
  assert.strictEqual(timers.pending.filter(Boolean).length, 0, 'no grace timer');
});

test('mouseenter also shows (fallback path)', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const timers = mockTimers();
  const tip = IT.attach(host, 'Identified', {
    doc: doc, mount: doc.body, timers: timers, hitGroup: true,
  });
  host.dispatch('mouseenter', { clientX: 20, clientY: 45 });
  assert.strictEqual(IT.isVisible(tip), true);
  host.dispatch('mouseleave', { clientX: 900, clientY: 900 });
  assert.strictEqual(IT.isVisible(tip), false);
});

test('empty text → no tip, no attach', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  assert.strictEqual(IT.attach(host, '', { doc: doc }), null);
  assert.strictEqual(IT.attach(host, '   ', { doc: doc }), null);
});

test('instant_tip.js source: 0ms show, HIDE_GRACE_MS=0, no bridge product path', function () {
  const src = fs.readFileSync(path.join(__dirname, 'instant_tip.js'), 'utf8');
  assert.ok(!/setTimeout\s*\(\s*show/.test(src), 'must not setTimeout before show');
  assert.ok(/SHOW_DELAY_MS\s*=\s*0/.test(src), 'SHOW_DELAY_MS = 0');
  assert.ok(/HIDE_GRACE_MS\s*=\s*0/.test(src), 'HIDE_GRACE_MS = 0 product');
  assert.ok(src.indexOf('pointInHitRect') >= 0, 'pointInHitRect pure predicate');
  assert.ok(src.indexOf('computeHitRect') >= 0 || src.indexOf('unionHitRect') >= 0,
    'single hit rect builder');
  // Product model must not use multi-element bridge path.
  assert.ok(src.indexOf('BRIDGE_CLASS') < 0, 'no BRIDGE_CLASS');
  assert.ok(src.indexOf('instant-tip-bridge') < 0, 'no instant-tip-bridge');
  assert.ok(src.indexOf('overBridge') < 0, 'no overBridge multi-flag');
  assert.ok(src.indexOf('pointerenter') >= 0);
  assert.ok(src.indexOf('dismissOtherTips') >= 0);
  assert.ok(src.indexOf('_instantTipForceHide') >= 0);
  assert.ok(!/setInterval\s*\(/.test(src), 'no setInterval');
});

test('index.html wires InstantTip on frontier cells; no title= for status/fanout/name', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('scripts/instant_tip.js') >= 0, 'script tag');
  assert.ok(html.indexOf('InstantTip.attach') >= 0, 'attach used');
  const start = html.indexOf('// 🎯T173: headerless table');
  assert.ok(start >= 0, 'frontier render marker');
  const end = html.indexOf('function loadFrontier', start);
  assert.ok(end > start, 'loadFrontier after render');
  const region = html.slice(start, end);
  assert.ok(region.indexOf('status.title') < 0, 'no status.title');
  assert.ok(region.indexOf('fan.title') < 0, 'no fan.title');
  assert.ok(region.indexOf('name.title') < 0, 'no name.title');
  assert.ok(/InstantTip\.attach\(\s*status/.test(region) || region.indexOf('InstantTip.attach(status') >= 0,
    'status InstantTip');
  assert.ok(region.indexOf('InstantTip.attach(fan') >= 0 || /InstantTip\.attach\(\s*fan/.test(region),
    'fan InstantTip');
  assert.ok(/\.instant-tip\s*\{/.test(html), 'instant-tip CSS');
  // 🎯T231: single hit rect wiring, no bridge CSS class as product path.
  assert.ok(region.indexOf('hitGroup') >= 0, 'hitGroup on cards');
  assert.ok(region.indexOf('groupHosts') >= 0, 'groupHosts id+name');
  assert.ok(html.indexOf('instant-tip-bridge') < 0, 'no bridge CSS');
  assert.ok(/\.instant-tip-hit\s*\{/.test(html) || html.indexOf('instant-tip-hit') >= 0,
    'hit layer CSS');
});

// ─── Placement (T181/T186) ────────────────────────────────────────────────

test('placeLeftOfPointerRect prefers left of cursor; flips when clipped (🎯T181)', function () {
  const roomy = IT.placeLeftOfPointerRect({
    pointerX: 400, pointerY: 300, tipW: 200, tipH: 100,
    viewW: 1000, viewH: 800, gap: 12, pad: 4,
  });
  assert.strictEqual(roomy.side, 'left');
  assert.strictEqual(roomy.left, 400 - 12 - 200);
  assert.strictEqual(roomy.top, 300 - 50);

  const edge = IT.placeLeftOfPointerRect({
    pointerX: 40, pointerY: 200, tipW: 200, tipH: 80,
    viewW: 800, viewH: 600, gap: 12, pad: 4,
  });
  assert.strictEqual(edge.side, 'right');
  assert.strictEqual(edge.left, 40 + 12);

  const topClamp = IT.placeLeftOfPointerRect({
    pointerX: 500, pointerY: 10, tipW: 100, tipH: 80,
    viewW: 800, viewH: 600, gap: 12, pad: 4,
  });
  assert.ok(topClamp.top >= 4, 'top clamped');
});

test('attach html + left-of-pointer shows immediately with card class (🎯T181)', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const tip = IT.attach(host, '<p>Acceptance A</p>', {
    doc: doc,
    mount: doc.body,
    html: true,
    ariaLabel: '🎯T181 Card. Acceptance A',
    placement: IT.PLACE_LEFT_OF_POINTER,
    className: IT.CARD_CLASS,
    clampSelectors: [],
    hitGroup: true,
  });
  assert.ok(tip);
  assert.ok(tip.className.indexOf(IT.CARD_CLASS) >= 0, 'card class on tip');
  assert.ok(tip.innerHTML.indexOf('Acceptance A') >= 0, 'html body has acceptance');
  host.dispatch('pointerenter', { clientX: 400, clientY: 250 });
  assert.strictEqual(IT.isVisible(tip), true, '0ms show');
  assert.ok(tip.style.left, 'left set');
  assert.strictEqual(tip.style.pointerEvents, 'auto', 'pointer-events auto while shown');
});

test('placeLeftOfPointerRect clamps right edge to maxRight (🎯T186)', function () {
  const pos = IT.placeLeftOfPointerRect({
    pointerX: 520, pointerY: 200, tipW: 200, tipH: 100,
    viewW: 1000, viewH: 800, gap: 12, pad: 4, maxRight: 500,
  });
  assert.strictEqual(pos.side, 'left');
  assert.ok(pos.left + 200 <= 500 + 0.5, 'right edge ≤ maxRight');
  assert.strictEqual(pos.left, 300);
});

test('resolveClampRight from clampRect / maxRight (🎯T186)', function () {
  assert.strictEqual(IT.resolveClampRight({ maxRight: 420 }), 420);
  assert.strictEqual(
    IT.resolveClampRight({ clampRect: { left: 600 }, clampGap: 8 }),
    592
  );
  assert.strictEqual(IT.DEFAULT_CLAMP_GAP, 8);
  assert.ok(IT.DEFAULT_CLAMP_SELECTORS.indexOf('#frontier-table') >= 0);
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
    hitGroup: true,
  });
  tip.offsetWidth = 200;
  tip.offsetHeight = 120;
  host.dispatch('pointerenter', { clientX: 480, clientY: 300 });
  assert.strictEqual(IT.isVisible(tip), true);
  const left = parseInt(tip.style.left, 10);
  assert.ok(left + 200 <= 350 + 0.5, 'card right ≤ maxRight: left=' + left);
});

test('index.html sticky + clamp wiring (🎯T186/T231)', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(/\.instant-tip\.instant-tip-show\s*\{[^}]*pointer-events:\s*auto/.test(html)
    || /instant-tip-show[\s\S]*?pointer-events:\s*auto/.test(html),
    'show class enables pointer-events');
  const start = html.indexOf('// 🎯T173: headerless table');
  assert.ok(start >= 0);
  const end = html.indexOf('function loadFrontier', start);
  const region = html.slice(start, end > start ? end : start + 9000);
  assert.ok(region.indexOf('sticky') >= 0 || region.indexOf('T186') >= 0 || region.indexOf('T231') >= 0,
    'sticky/T186/T231 in render');
  assert.ok(region.indexOf('clampSelectors') >= 0, 'clampSelectors wired');
  assert.ok(region.indexOf('hitGroup') >= 0, 'hitGroup wired');
});

// ─── Singleton (T203) ─────────────────────────────────────────────────────

test('singleton: attach/show second tip hides first (🎯T203)', function () {
  const doc = mockDoc();
  const host1 = mockEl('td');
  const host2 = mockEl('td');
  host1.ownerDocument = doc;
  host2.ownerDocument = doc;
  const tip1 = IT.attach(host1, '<p>Card A — T100</p>', {
    doc: doc, mount: doc.body, html: true,
    sticky: true, hitGroup: true, className: IT.CARD_CLASS,
    placement: IT.PLACE_LEFT_OF_POINTER, clampSelectors: [],
  });
  const tip2 = IT.attach(host2, '<p>Card B — T200</p>', {
    doc: doc, mount: doc.body, html: true,
    sticky: true, hitGroup: true, className: IT.CARD_CLASS,
    placement: IT.PLACE_LEFT_OF_POINTER, clampSelectors: [],
  });
  assert.ok(tip1 && tip2);
  assert.strictEqual(typeof tip1._instantTipForceHide, 'function');

  host1.dispatch('pointerenter', { clientX: 400, clientY: 200 });
  assert.strictEqual(IT.isVisible(tip1), true, 'first shown');
  assert.strictEqual(IT.openTipsCount(), 1);

  host2.dispatch('pointerenter', { clientX: 420, clientY: 220 });
  assert.strictEqual(IT.isVisible(tip2), true, 'second shown');
  assert.strictEqual(IT.isVisible(tip1), false, 'first hidden by singleton');
  assert.strictEqual(IT.openTipsCount(), 1);
  IT.hideTip(tip2);
});

test('singleton: showTip path dismisses peers without attach sticky (🎯T203)', function () {
  const doc = mockDoc();
  const a = mockEl('div');
  const b = mockEl('div');
  a.className = IT.TIP_CLASS;
  b.className = IT.TIP_CLASS;
  a.style = {};
  b.style = {};
  IT.showTip(a, null, {});
  assert.strictEqual(IT.isVisible(a), true);
  IT.showTip(b, null, {});
  assert.strictEqual(IT.isVisible(b), true);
  assert.strictEqual(IT.isVisible(a), false, 'peer hidden on showTip');
  IT.hideTip(b);
  assert.strictEqual(IT.openTipsCount(), 0);
});

// ─── T187/T230 retained: no idle timeout over card ────────────────────────

test('T187 pure: shouldRunScheduledHide never true while over tip/host', function () {
  assert.strictEqual(IT.shouldRunScheduledHide({ overTip: true, overHost: false }), false);
  assert.strictEqual(IT.shouldRunScheduledHide({ overTip: false, overHost: true }), false);
  assert.strictEqual(IT.shouldRunScheduledHide({ insideHitRect: true }), false);
  assert.strictEqual(IT.shouldRunScheduledHide({ overTip: false, overHost: false }), true);
});

test('T187 hideSchedule: product hit-rect, grace 0, no setInterval', function () {
  const hs = IT.hideSchedule({ hitGroup: true });
  assert.strictEqual(hs.usesInterval, false);
  assert.strictEqual(hs.graceMs, 0);
  assert.strictEqual(hs.usesTimeout, false);
  assert.strictEqual(hs.model, 'hit-rect');
  assert.strictEqual(hs.immediateOnLeaveHitGroup, true);
  assert.strictEqual(IT.HIDE_GRACE_MS, 0);
  assert.strictEqual(IT.resolveHideGraceMs({ hitGroup: true }), 0);
});

test('T187: over tip → stay open; leave tip outside rect → hide now', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  host._rect = { left: 320, top: 200, right: 360, bottom: 220, width: 40, height: 20 };
  const timers = mockTimers();
  const tip = IT.attach(host, 'Sticky card — no idle timeout', {
    doc: doc,
    mount: doc.body,
    sticky: true,
    hitGroup: true,
    timers: timers,
    placement: IT.PLACE_LEFT_OF_POINTER,
    className: IT.CARD_CLASS,
    clampSelectors: [],
  });
  // Inject known hit rect: card left + host right.
  tip._instantTipSetHitRect({ left: 100, top: 50, right: 360, bottom: 400 });
  host.dispatch('pointerenter', { clientX: 340, clientY: 210 });
  assert.strictEqual(IT.isVisible(tip), true);
  tip.dispatch('pointerenter', { clientX: 150, clientY: 100 });
  assert.strictEqual(tip._instantTipHoverState().overTip, true);

  // Idle flush — still open (no timeout).
  timers.flush();
  timers.flush();
  assert.strictEqual(IT.isVisible(tip), true, 'still open after idle flush over tip');
  assert.strictEqual(timers.pending.filter(Boolean).length, 0, 'no hide timer');

  // Leave host while over tip (still in rect) — stay.
  host.dispatch('pointerleave', { clientX: 150, clientY: 100, relatedTarget: tip });
  assert.strictEqual(IT.isVisible(tip), true, 'leave-host into tip keeps open');

  // Sample outside hit rect → dismiss now, 0 grace.
  tip._instantTipSamplePointer(900, 900);
  assert.strictEqual(IT.isVisible(tip), false, 'outside hit rect hides immediately');
  assert.strictEqual(timers.pending.filter(Boolean).length, 0, 'no grace after leave');
});

// ─── T230: latch + re-render ──────────────────────────────────────────────

test('T230 pure: isHoverLatchedState while inside hit rect / host / tip', function () {
  assert.strictEqual(IT.isHoverLatchedState({ overTip: true }, true), true);
  assert.strictEqual(IT.isHoverLatchedState({ overHost: true }, true), true);
  assert.strictEqual(IT.isHoverLatchedState({ insideHitRect: true }, true), true);
  assert.strictEqual(IT.isHoverLatchedState({ overTip: false, overHost: false }, true), false);
  assert.strictEqual(IT.isHoverLatchedState({ overTip: true }, false), false);
});

test('T230: still pointer over tip — latched; leave outside rect unlatches + hides', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const timers = mockTimers();
  const tip = IT.attach(host, '<p>Frontier card body</p>', {
    doc: doc, mount: doc.body, html: true, sticky: true, hitGroup: true,
    timers: timers, placement: IT.PLACE_LEFT_OF_POINTER, className: IT.CARD_CLASS,
    clampSelectors: [],
  });
  tip._instantTipSetHitRect({ left: 100, top: 50, right: 400, bottom: 300 });
  host.dispatch('pointerenter', { clientX: 200, clientY: 100 });
  tip.dispatch('pointerenter', { clientX: 150, clientY: 80 });
  assert.strictEqual(IT.isHoverLatched(tip), true, 'latched over tip');
  assert.strictEqual(IT.anyHoverLatched(), true);
  timers.flush();
  assert.strictEqual(IT.isVisible(tip), true, 'no idle auto-hide');

  tip._instantTipSamplePointer(800, 800);
  assert.strictEqual(IT.isVisible(tip), false);
  assert.strictEqual(IT.anyHoverLatched(), false);
});

test('T230: discardDetachedTips preserves latched tip, removes others', function () {
  const doc = mockDoc();
  const body = doc.body;
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
    doc: doc, mount: body, sticky: true, hitGroup: true,
  });
  const tipB = IT.attach(hostB, 'Card B closed', {
    doc: doc, mount: body, sticky: true, hitGroup: true,
  });
  tips.push(tipA, tipB);

  hostA.dispatch('pointerenter', { clientX: 20, clientY: 45 });
  tipA.dispatch('pointerenter', { clientX: 20, clientY: 45 });
  assert.strictEqual(IT.isHoverLatched(tipA), true);
  assert.strictEqual(IT.isHoverLatched(tipB), false);

  const r = IT.discardDetachedTips(doc);
  assert.strictEqual(r.preserved, 1, 'latched preserved');
  assert.strictEqual(r.removed, 1, 'closed removed');
  assert.strictEqual(IT.isVisible(tipA), true);
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
});

// ─── 🎯T231: single hit rect = AABB(card ∪ id+name), grace 0 ─────────────

test('T231 pure: unionHitRect is one AABB of card + hosts', function () {
  const card = { left: 100, top: 50, right: 300, bottom: 400 };
  const id = { left: 320, top: 200, right: 360, bottom: 220 };
  const name = { left: 360, top: 200, right: 500, bottom: 220 };
  const u = IT.unionHitRect([card, id, name]);
  assert.strictEqual(u.left, 100);
  assert.strictEqual(u.top, 50);
  assert.strictEqual(u.right, 500);
  assert.strictEqual(u.bottom, 400);
});

test('T231 pure: computeHitRect = AABB(card ∪ hostRects) — no multi-region', function () {
  const rect = IT.computeHitRect({
    cardRect: { left: 100, top: 50, right: 300, bottom: 400 },
    hostRects: [
      { left: 320, top: 200, right: 360, bottom: 220 },
      { left: 360, top: 200, right: 500, bottom: 220 },
    ],
  });
  assert.ok(rect);
  assert.strictEqual(rect.left, 100);
  assert.strictEqual(rect.top, 50);
  assert.strictEqual(rect.right, 500);
  assert.strictEqual(rect.bottom, 400);
  // Single rect only — computeHitRect returns one object, not an array of bridges.
  assert.strictEqual(typeof rect.left, 'number');
  assert.ok(!Array.isArray(rect));
});

test('T231 pure: pointInHitRect / shouldDismissOutsideHitRect', function () {
  const rect = { left: 100, top: 50, right: 500, bottom: 400 };
  assert.strictEqual(IT.pointInHitRect(200, 100, rect), true, 'inside card');
  assert.strictEqual(IT.pointInHitRect(400, 210, rect), true, 'inside id/name band');
  assert.strictEqual(IT.pointInHitRect(310, 210, rect), true, 'gap inside AABB stays open');
  assert.strictEqual(IT.pointInHitRect(310, 30, rect), false, 'above hit rect top');
  assert.strictEqual(IT.pointInHitRect(310, 450, rect), false, 'below hit rect bottom');
  assert.strictEqual(IT.pointInHitRect(50, 210, rect), false, 'left of card');
  assert.strictEqual(IT.pointInHitRect(600, 210, rect), false, 'right of name');
  assert.strictEqual(IT.shouldDismissOutsideHitRect(310, 30, rect), true);
  assert.strictEqual(IT.shouldDismissOutsideHitRect(200, 100, rect), false);
});

test('T231 pure: HIDE_GRACE_MS is 0 for continuous hit-rect product', function () {
  assert.strictEqual(IT.HIDE_GRACE_MS, 0);
  assert.strictEqual(IT.resolveHideGraceMs({ hitGroup: true }), 0);
  assert.strictEqual(IT.resolveHideGraceMs({}), 0);
  const hs = IT.hideSchedule({ hitGroup: true });
  assert.strictEqual(hs.graceMs, 0);
  assert.strictEqual(hs.usesTimeout, false);
});

test('T231: leave hit rect → dismiss immediately (0 grace, no timer)', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  host._rect = { left: 320, top: 200, right: 500, bottom: 220, width: 180, height: 20 };
  const timers = mockTimers();
  const tip = IT.attach(host, '<p>Card</p>', {
    doc: doc, mount: doc.body, html: true, sticky: true, hitGroup: true,
    timers: timers, className: IT.CARD_CLASS, placement: IT.PLACE_LEFT_OF_POINTER,
    clampSelectors: [],
  });
  tip._instantTipSetHitRect({ left: 100, top: 50, right: 500, bottom: 400 });
  host.dispatch('pointerenter', { clientX: 400, clientY: 210 });
  assert.strictEqual(IT.isVisible(tip), true);

  // Sample outside — immediate hide, zero pending timers.
  const stayed = tip._instantTipSamplePointer(900, 100);
  assert.strictEqual(stayed, false);
  assert.strictEqual(IT.isVisible(tip), false, 'dismiss now');
  assert.strictEqual(timers.pending.filter(Boolean).length, 0, '0 grace — no timer');
});

test('T231: above hit-rect top while over strip-x → dismiss', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const timers = mockTimers();
  const tip = IT.attach(host, 'Card', {
    doc: doc, mount: doc.body, sticky: true, hitGroup: true, timers: timers,
  });
  // Hit rect top=50; sample y=30 at x inside horizontal span → outside.
  tip._instantTipSetHitRect({ left: 100, top: 50, right: 500, bottom: 400 });
  host.dispatch('pointerenter', { clientX: 300, clientY: 100 });
  assert.strictEqual(IT.isVisible(tip), true);
  tip._instantTipSamplePointer(310, 30);
  assert.strictEqual(IT.isVisible(tip), false, 'above rect dismisses');
  assert.strictEqual(timers.pending.filter(Boolean).length, 0);
});

test('T231: below hit-rect bottom while over strip-x → dismiss', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const timers = mockTimers();
  const tip = IT.attach(host, 'Card', {
    doc: doc, mount: doc.body, sticky: true, hitGroup: true, timers: timers,
  });
  tip._instantTipSetHitRect({ left: 100, top: 50, right: 500, bottom: 400 });
  host.dispatch('pointerenter', { clientX: 300, clientY: 100 });
  tip._instantTipSamplePointer(310, 450);
  assert.strictEqual(IT.isVisible(tip), false, 'below rect dismisses');
  assert.strictEqual(timers.pending.filter(Boolean).length, 0);
});

test('T231: id/name → card (still inside hit rect) stays open', function () {
  const doc = mockDoc();
  const id = mockEl('td');
  const name = mockEl('td');
  id.ownerDocument = doc;
  name.ownerDocument = doc;
  id._rect = { left: 320, top: 200, right: 360, bottom: 220, width: 40, height: 20 };
  name._rect = { left: 360, top: 200, right: 500, bottom: 220, width: 140, height: 20 };
  const timers = mockTimers();
  const tip = IT.attach(id, '<p>Rich card</p>', {
    doc: doc, mount: doc.body, html: true, sticky: true, hitGroup: true,
    groupHosts: [id, name], timers: timers, className: IT.CARD_CLASS,
    placement: IT.PLACE_LEFT_OF_POINTER, clampSelectors: [],
  });
  tip._instantTipSetHitRect({ left: 100, top: 50, right: 500, bottom: 400 });
  tip.offsetWidth = 200;
  tip.offsetHeight = 350;
  tip.style.left = '100px';
  tip.style.top = '50px';

  // Enter id cell.
  id.dispatch('pointerenter', { clientX: 340, clientY: 210 });
  assert.strictEqual(IT.isVisible(tip), true);

  // Move through gap (inside AABB) toward card — sample stays inside.
  assert.strictEqual(tip._instantTipSamplePointer(310, 210), true, 'gap inside AABB');
  assert.strictEqual(IT.isVisible(tip), true);

  // Onto card body.
  assert.strictEqual(tip._instantTipSamplePointer(150, 100), true, 'on card');
  assert.strictEqual(IT.isVisible(tip), true);
  tip.dispatch('pointerenter', { clientX: 150, clientY: 100 });
  assert.strictEqual(IT.isVisible(tip), true, 'tip enter keeps open');

  // Leave host while relatedTarget is tip — stay.
  id.dispatch('pointerleave', { clientX: 150, clientY: 100, relatedTarget: tip });
  assert.strictEqual(IT.isVisible(tip), true, 'host→card no dismiss');
  assert.strictEqual(timers.pending.filter(Boolean).length, 0, 'no grace timer armed');
});

test('T231: leave card into void outside rect → dismiss 0 grace', function () {
  const doc = mockDoc();
  const host = mockEl('td');
  host.ownerDocument = doc;
  const timers = mockTimers();
  const tip = IT.attach(host, 'Card', {
    doc: doc, mount: doc.body, sticky: true, hitGroup: true, timers: timers,
  });
  tip._instantTipSetHitRect({ left: 100, top: 50, right: 500, bottom: 400 });
  host.dispatch('pointerenter', { clientX: 200, clientY: 100 });
  tip.dispatch('pointerenter', { clientX: 150, clientY: 80 });
  assert.strictEqual(IT.isVisible(tip), true);

  // Leave tip with coords outside rect.
  tip.dispatch('pointerleave', { clientX: 50, clientY: 80 });
  assert.strictEqual(IT.isVisible(tip), false, 'leave card outside rect hides');
  assert.strictEqual(timers.pending.filter(Boolean).length, 0);
});

test('T231: source has no multi-element bridge product model', function () {
  const src = fs.readFileSync(path.join(__dirname, 'instant_tip.js'), 'utf8');
  assert.ok(src.indexOf('pointInHitRect') >= 0);
  assert.ok(src.indexOf('unionHitRect') >= 0 || src.indexOf('computeHitRect') >= 0);
  assert.ok(src.indexOf('instant-tip-bridge') < 0);
  assert.ok(src.indexOf('BRIDGE_CLASS') < 0);
  assert.ok(src.indexOf('overBridge') < 0);
  assert.ok(src.indexOf('bridgeRectBetween') < 0, 'no bridge geometry helper');
  assert.ok(/HIDE_GRACE_MS\s*=\s*0/.test(src));
  // index: single attach + groupHosts, no dual name attach for cards.
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const start = html.indexOf('// 🎯T173: headerless table');
  const end = html.indexOf('function loadFrontier', start);
  const region = html.slice(start, end);
  assert.ok(region.indexOf('groupHosts') >= 0);
  assert.ok(region.indexOf('hitGroup') >= 0);
  assert.ok(region.indexOf('instant-tip-bridge') < 0);
  assert.ok(!/InstantTip\.attach\(\s*name/.test(region)
    || region.indexOf('groupHosts') >= 0,
    'card path uses groupHosts not dual name attach required');
});

test('T231 index wiring: hitGroup + groupHosts + no bridge', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('hitGroup: true') >= 0 || html.indexOf('hitGroup:true') >= 0
    || /hitGroup:\s*true/.test(html), 'hitGroup true on cards');
  assert.ok(html.indexOf('groupHosts') >= 0);
  assert.ok(html.indexOf('instant-tip-bridge') < 0);
  assert.ok(html.indexOf('T231') >= 0);
});

// ─── 🎯T271: multi-part region — vertical leave on table dismisses ───────

test('T271 pure: corridor is horizontal gap only (not tall AABB fill)', function () {
  const card = { left: 100, top: 50, right: 300, bottom: 400 };
  const hosts = [
    { left: 320, top: 200, right: 360, bottom: 220 },
    { left: 360, top: 200, right: 500, bottom: 220 },
  ];
  const corridor = IT.bridgeCorridorBetween(card, hosts);
  assert.ok(corridor, 'corridor exists');
  assert.strictEqual(corridor.left, 300);
  assert.strictEqual(corridor.right, 320);
  assert.strictEqual(corridor.top, 50);
  assert.strictEqual(corridor.bottom, 400);
  // Table-side point (same X as hosts, Y within card but off row) is NOT in corridor.
  assert.strictEqual(IT.pointInHitRect(400, 100, corridor), false, 'table not corridor');
});

test('T271 pure: pointInHitParts = card ∪ hosts ∪ corridor (not filled AABB)', function () {
  const parts = IT.computeHitParts({
    cardRect: { left: 100, top: 50, right: 300, bottom: 400 },
    hostRects: [
      { left: 320, top: 200, right: 360, bottom: 220 },
      { left: 360, top: 200, right: 500, bottom: 220 },
    ],
  });
  assert.ok(parts.card);
  assert.ok(parts.hosts && parts.hosts.length === 2);
  assert.ok(parts.corridor);
  // Inside card / hosts / corridor → stay
  assert.strictEqual(IT.pointInHitParts(200, 100, parts), true, 'on card');
  assert.strictEqual(IT.pointInHitParts(340, 210, parts), true, 'on id');
  assert.strictEqual(IT.pointInHitParts(400, 210, parts), true, 'on name');
  assert.strictEqual(IT.pointInHitParts(310, 210, parts), true, 'corridor at host Y');
  assert.strictEqual(IT.pointInHitParts(310, 100, parts), true, 'corridor tall span');
  // Tall AABB would keep these open — multi-part must dismiss (vertical leave on table)
  assert.strictEqual(IT.pointInHitParts(400, 100, parts), false, 'above row on table');
  assert.strictEqual(IT.pointInHitParts(400, 350, parts), false, 'below row on table');
  assert.strictEqual(IT.pointInHitParts(340, 30, parts), false, 'above envelope');
  assert.strictEqual(IT.pointInHitParts(340, 450, parts), false, 'below envelope');
  assert.strictEqual(IT.shouldDismissOutsideHitParts(400, 100, parts), true);
  assert.strictEqual(IT.shouldDismissOutsideHitParts(200, 100, parts), false);
});

test('T271: vertical leave over other table rows dismisses (product path, no inject)', function () {
  const doc = mockDoc();
  const id = mockEl('td');
  const name = mockEl('td');
  id.ownerDocument = doc;
  name.ownerDocument = doc;
  // Hosts: row band y=200–220, x=320–500
  id._rect = { left: 320, top: 200, right: 360, bottom: 220, width: 40, height: 20 };
  name._rect = { left: 360, top: 200, right: 500, bottom: 220, width: 140, height: 20 };
  const timers = mockTimers();
  const tip = IT.attach(id, '<p>Rich card</p>', {
    doc: doc, mount: doc.body, html: true, sticky: true, hitGroup: true,
    groupHosts: [id, name], timers: timers, className: IT.CARD_CLASS,
    placement: IT.PLACE_LEFT_OF_POINTER, clampSelectors: [],
  });
  // Tall card left of hosts (product placement)
  tip.offsetWidth = 200;
  tip.offsetHeight = 350;
  tip.style.left = '100px';
  tip.style.top = '50px';
  tip._rect = { left: 100, top: 50, right: 300, bottom: 400, width: 200, height: 350 };
  tip.getBoundingClientRect = function () { return Object.assign({}, tip._rect); };

  id.dispatch('pointerenter', { clientX: 340, clientY: 210 });
  assert.strictEqual(IT.isVisible(tip), true, 'open on id');

  // Host → card via corridor stays open
  assert.strictEqual(tip._instantTipSamplePointer(310, 210), true, 'corridor stay');
  assert.strictEqual(IT.isVisible(tip), true);
  assert.strictEqual(tip._instantTipSamplePointer(150, 100), true, 'on card stay');
  assert.strictEqual(IT.isVisible(tip), true);

  // Vertical leave: same host X, Y within tall card span but off the row → dismiss
  // (old AABB(card∪hosts) would keep this open — T271 regression fix)
  assert.strictEqual(tip._instantTipSamplePointer(400, 100), false, 'above row on table');
  assert.strictEqual(IT.isVisible(tip), false, 'vertical leave dismisses');
  assert.strictEqual(timers.pending.filter(Boolean).length, 0, '0 grace');
});

test('T271: below row on table dismisses; id/name stay', function () {
  const doc = mockDoc();
  const id = mockEl('td');
  id.ownerDocument = doc;
  id._rect = { left: 320, top: 200, right: 500, bottom: 220, width: 180, height: 20 };
  const timers = mockTimers();
  const tip = IT.attach(id, 'Card', {
    doc: doc, mount: doc.body, sticky: true, hitGroup: true, timers: timers,
    placement: IT.PLACE_LEFT_OF_POINTER, clampSelectors: [],
  });
  tip.offsetWidth = 200;
  tip.offsetHeight = 350;
  tip.style.left = '100px';
  tip.style.top = '50px';
  tip._rect = { left: 100, top: 50, right: 300, bottom: 400, width: 200, height: 350 };
  tip.getBoundingClientRect = function () { return Object.assign({}, tip._rect); };

  id.dispatch('pointerenter', { clientX: 400, clientY: 210 });
  assert.strictEqual(IT.isVisible(tip), true);
  assert.strictEqual(tip._instantTipSamplePointer(400, 210), true, 'on host stay');
  assert.strictEqual(tip._instantTipSamplePointer(400, 350), false, 'below row on table');
  assert.strictEqual(IT.isVisible(tip), false);
  assert.strictEqual(timers.pending.filter(Boolean).length, 0);
});

test('T271 source: multi-part product path present', function () {
  const src = fs.readFileSync(path.join(__dirname, 'instant_tip.js'), 'utf8');
  assert.ok(src.indexOf('pointInHitParts') >= 0);
  assert.ok(src.indexOf('computeHitParts') >= 0);
  assert.ok(src.indexOf('bridgeCorridorBetween') >= 0);
  assert.ok(src.indexOf('T271') >= 0);
  assert.ok(/HIDE_GRACE_MS\s*=\s*0/.test(src));
  assert.ok(src.indexOf('instant-tip-bridge') < 0);
  assert.ok(src.indexOf('overBridge') < 0);
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('All instant_tip tests passed');
