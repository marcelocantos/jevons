// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

const assert = require('node:assert/strict');

// Passed to page.evaluate: use a real browser socket, without intercepting
// React's connection or manufacturing received transcript events. A separate
// socket submits only; the packaged UI's existing subscription observes echo.
async function submitAside({ name, text }) {
  const url = new URL('/ws/mux', location.href);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  return new Promise((resolve, reject) => {
    const socket = new WebSocket(url.href);
    const ch = `transcript:${name}`;
    let settled = false;
    const settle = (error, status) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      socket.close();
      if (error) reject(error); else resolve(status);
    };
    const timer = setTimeout(() => settle(new Error('aside mux submit status timed out')), 90000);
    socket.onopen = () => socket.send(JSON.stringify({ v: 1, ch, t: 'send', body: { text, mode: 'submit' } }));
    socket.onerror = () => settle(new Error('aside mux submit socket error'));
    socket.onclose = () => settle(new Error('aside mux submit closed before status'));
    socket.onmessage = ({ data }) => {
      let frame;
      try { frame = JSON.parse(data); } catch { return; }
      if (frame.ch !== ch) return;
      if (frame.t === 'error') settle(new Error(`aside mux submit: ${JSON.stringify(frame.body)}`));
      if (frame.t === 'status') settle(null, frame.body);
    };
  });
}

// Shared by J31's wait and its hermetic negative controls. No echo remains
// pending (and times out in J31); a misplaced echo is an immediate failure.
function ownerEcho(events, first, owner) {
  const text = event => {
    const c = event?.message?.content;
    return typeof c === 'string' ? c : (c || []).filter(b => b.type === 'text' || !b.type).map(b => b.text || '').join('');
  };
  const echo = events.find(f => f.body?.event?.type === 'user' &&
    f.body.event.turn_origin === 'owner' && text(f.body.event).includes(owner));
  if (!echo) return undefined;
  assert(echo.body.index > first.body.index, 'owner must follow PRE');
  assert(!events.some(f => f.body?.event?.type === 'assistant' &&
    ['end_turn', 'stop_sequence'].includes(f.body.event.message?.stop_reason)),
  'first response ended before second owner echo');
  return echo;
}

module.exports = { submitAside, ownerEcho };
