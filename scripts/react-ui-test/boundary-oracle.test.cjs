// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

const assert = require('node:assert/strict');
const { test } = require('node:test');
const { ownerEcho, submitAside } = require('./boundary-oracle.cjs');
const frame = (index, type, text, extra = {}) => ({ body: { index, event: { type, message: { content: text }, ...extra } } });
const pre = frame(10, 'assistant', 'PRE');
const owner = 'Reply with exactly: ACK';
const echo = frame(11, 'user', owner, { turn_origin: 'owner' });

test('T813 accepts an owner echo after nonterminal PRE', () => {
  assert.equal(ownerEcho([pre, echo], pre, owner), echo);
});
test('T813 missing owner echo cannot satisfy the journey wait', () => {
  const agent = frame(11, 'user', owner, { turn_origin: 'agent' });
  const quoted = frame(12, 'assistant', owner, { turn_origin: 'owner' });
  assert.equal(ownerEcho([pre, agent, quoted], pre, owner), undefined);
});
test('T813 out-of-order owner echo fails the journey oracle', () => {
  for (const index of [9, 10]) {
    assert.throws(() => ownerEcho([pre, { ...echo, body: { ...echo.body, index } }], pre, owner), /owner must follow PRE/);
  }
});
test('T813 terminal PRE before the owner echo fails the journey oracle', () => {
  const ended = frame(10, 'assistant', 'PRE');
  ended.body.event.message.stop_reason = 'end_turn';
  assert.throws(() => ownerEcho([ended, echo], ended, owner), /first response ended/);
});
test('T813 direct aside submit uses submit mode and awaits daemon status', async t => {
  t.mock.method(globalThis, 'setTimeout', () => 1);
  t.mock.method(globalThis, 'clearTimeout', () => {});
  const oldLocation = globalThis.location, oldSocket = globalThis.WebSocket;
  t.after(() => { globalThis.location = oldLocation; globalThis.WebSocket = oldSocket; });
  globalThis.location = { href: 'http://127.0.0.1:23456/?agent=aside' };
  let socket;
  globalThis.WebSocket = class {
    constructor(url) { this.url = url; socket = this; }
    send(data) { this.sent = JSON.parse(data); }
    close() { this.closed = true; }
  };
  const pending = submitAside({ name: 'aside', text: owner });
  socket.onopen();
  assert.equal(socket.url, 'ws://127.0.0.1:23456/ws/mux');
  assert.deepEqual(socket.sent, { v: 1, ch: 'transcript:aside', t: 'send', body: { text: owner, mode: 'submit' } });
  socket.onmessage({ data: JSON.stringify({ ch: 'transcript:aside', t: 'status', body: { status: 'queued', mode: 'submit' } }) });
  assert.deepEqual(await pending, { status: 'queued', mode: 'submit' });
  assert.equal(socket.closed, true);
});
