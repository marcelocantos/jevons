// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic oracle for 🎯T509 envelope parse + cockpit markdown-pipeline rewrite.
// Run: node web/scripts/jevons_envelope_test.js

'use strict';

const assert = require('assert');
const JevonsEnvelope = require('./jevons_envelope.js');

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

const FINISH = [
  '```jevons',
  'jevons: kind finish-report',
  'jevons: target T509',
  'jevons: oracle sha=abcdef0123456 gate-id=9f13c0a2',
  'jevons: verdict GREEN',
  'jevons: status in-progress',
  '```',
  '',
  'Work landed. SHA abcdef0123456.',
].join('\n');

test('parse reads kind, target, verdict, status from a line-1 fence', () => {
  const p = JevonsEnvelope.parse(FINISH);
  assert.ok(p && p.slots, 'parsed');
  assert.strictEqual(p.slots.kind, 'finish-report');
  assert.strictEqual(p.slots.target, 'T509');
  assert.strictEqual(p.slots.verdict, 'GREEN');
  assert.strictEqual(p.slots.status, 'in-progress');
  assert.strictEqual(p.slots.sha, 'abcdef0123456');
  assert.ok(p.payload.indexOf('Work landed') >= 0);
  assert.ok(!p.incomplete);
});

test('YAML front matter is not an envelope (the format we refused)', () => {
  const yaml = '---\nkind: finish-report\ntarget: T509\n---\n\nDone.';
  assert.strictEqual(JevonsEnvelope.parse(yaml), null);
  const stripped = JevonsEnvelope.stripYamlFrontMatter(yaml);
  assert.ok(stripped.indexOf('kind: finish-report') < 0, 'YAML would vanish in a front-matter renderer');
  assert.ok(stripped.indexOf('Done.') >= 0);
});

test('rewrite replaces the fence with a compact visible header, not a code dump', () => {
  const out = JevonsEnvelope.rewrite(FINISH);
  assert.ok(out.indexOf('class="jevons-envelope"') >= 0, 'header element');
  assert.ok(out.indexOf('finish-report') >= 0, 'kind is visible text');
  assert.ok(out.indexOf('T509') >= 0, 'target is visible text');
  assert.ok(out.indexOf('GREEN') >= 0, 'verdict is visible text');
  assert.ok(out.indexOf('```jevons') < 0, 'fence opener removed so marked will not dump a <pre>');
  assert.ok(out.indexOf('jevons: kind') < 0, 'slot lines are not dumped');
  assert.ok(out.indexOf('Work landed') >= 0, 'payload remains');
});

test('rewrite is idempotent on an already-rewritten header', () => {
  const once = JevonsEnvelope.rewrite(FINISH);
  const twice = JevonsEnvelope.rewrite(once);
  assert.strictEqual(twice, once);
});

test('incomplete fence (mid-stream) is left alone', () => {
  const mid = '```jevons\njevons: kind finish-report\njevons: target T509\n';
  assert.strictEqual(JevonsEnvelope.rewrite(mid), mid);
});

test('quoted fence deeper than line 1 is not rewritten', () => {
  const quoted = 'Earlier they wrote:\n\n```jevons\njevons: kind ack\n```\n\nthat was them.';
  assert.strictEqual(JevonsEnvelope.rewrite(quoted), quoted);
});

test('header survives after [Agent … responded] prefix', () => {
  const wrapped = '[Agent jv-t509-envelopes responded]\n' + FINISH;
  const out = JevonsEnvelope.rewrite(wrapped);
  assert.ok(out.indexOf('class="jevons-envelope"') >= 0);
  assert.ok(out.indexOf('finish-report') >= 0);
});

test('unenveloped prose is unchanged', () => {
  const prose = 'Done. SHA abcdef0. go test PASS';
  assert.strictEqual(JevonsEnvelope.rewrite(prose), prose);
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('all ok');
