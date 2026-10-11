// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { join } = require('node:path');
const vm = require('node:vm');

test('T1057 inspector probe observes only key metadata and selection; stop removes listeners', () => {
  const listeners = new Map();
  const logs = [];
  class Textarea {
    constructor() {
      this.selectionStart = 7;
      this.selectionEnd = 7;
      this.dataset = { composer: 'main' };
      this.value = 'secret draft must never be logged';
    }
    matches(selector) { return selector === 'textarea[data-composer]'; }
  }
  const textarea = new Textarea();
  const document = {
    activeElement: textarea,
    addEventListener(name, callback) { listeners.set(name, callback); },
    removeEventListener(name, callback) { if (listeners.get(name) === callback) listeners.delete(name); },
  };
  const window = {};
  const timers = [];
  const source = readFileSync(join(__dirname, 't1057-key-boundary.js'), 'utf8');
  vm.runInNewContext(source, {
    window, document, HTMLTextAreaElement: Textarea,
    console: { log(...args) { logs.push(args); } },
    setTimeout(callback) { timers.push(callback); return timers.length; },
    clearTimeout() {},
  });
  assert.deepEqual([...listeners.keys()].sort(), ['beforeinput', 'input', 'keydown']);
  let cancelled = false;
  const key = {
    key: 'ArrowLeft', code: 'ArrowLeft', shiftKey: false, metaKey: false,
    ctrlKey: false, altKey: false, isTrusted: true, defaultPrevented: false,
    preventDefault() { cancelled = true; },
  };
  listeners.get('keydown')(key);
  textarea.selectionStart = 6;
  textarea.selectionEnd = 6;
  timers.forEach((callback) => callback());
  assert.equal(cancelled, false);
  assert.equal(window.__jevonsT1057Probe.rows[0].key, 'ArrowLeft');
  assert.equal(window.__jevonsT1057Probe.rows[0].after[0], 6);
  assert.equal(JSON.stringify(logs).includes(textarea.value), false);
  window.__jevonsT1057Probe.stop();
  assert.equal(listeners.size, 0);
  assert.equal(window.__jevonsT1057Probe, undefined);
});
