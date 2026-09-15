// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { afterEach, describe, expect, it, vi } from 'vitest';
import { CHAT_PING, HEARTBEAT_MS, MuxClient } from './client';

class FakeWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;
  static instances: FakeWebSocket[] = [];

  url: string;
  readyState = FakeWebSocket.CONNECTING;
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;

  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }

  send(data: string): void {
    this.sent.push(data);
  }

  close(): void {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.();
  }

  open(): void {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.();
  }
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  FakeWebSocket.instances = [];
});

function connectClient(): { client: MuxClient; ws: FakeWebSocket } {
  vi.stubGlobal('WebSocket', FakeWebSocket);
  vi.useFakeTimers();
  const client = new MuxClient('ws://test/ws/mux');
  client.connect();
  const ws = FakeWebSocket.instances[0];
  if (!ws) throw new Error('WebSocket was not constructed');
  ws.open();
  return { client, ws };
}

describe('MuxClient transcript watch refcount', () => {
  it('does not close a shared transcript until the last subscriber leaves', () => {
    const { client, ws } = connectClient();
    client.openTranscript('jevons');
    client.openTranscript('jevons');
    const opens = ws.sent.filter((s) => s.includes('"t":"open"'));
    expect(opens).toHaveLength(1);
    client.closeTranscript('jevons');
    expect(ws.sent.some((s) => s.includes('"t":"close"'))).toBe(false);
    client.closeTranscript('jevons');
    expect(ws.sent.filter((s) => s.includes('"t":"close"'))).toHaveLength(1);
    client.close();
  });
});

describe('MuxClient snapshot channels (T631)', () => {
  it('re-opens plan-usage on reconnect', () => {
    const { client, ws } = connectClient();
    client.openChannel('plan-usage');
    expect(ws.sent.some((s) => s.includes('"ch":"plan-usage"') && s.includes('"t":"open"'))).toBe(true);
    ws.close();
    vi.advanceTimersByTime(500);
    const next = FakeWebSocket.instances[1];
    if (!next) throw new Error('reconnect did not construct a socket');
    next.open();
    const opens = next.sent.filter((s) => s.includes('"ch":"plan-usage"') && s.includes('"t":"open"'));
    expect(opens).toHaveLength(1);
    client.close();
  });
});

describe('MuxClient interrupt (T644)', () => {
  it('sendTranscript carries interrupt; empty cancel is t=interrupt', () => {
    const { client, ws } = connectClient();
    client.sendTranscript('jevons', 'cut in', { mode: 'interrupt' });
    const send = ws.sent.find((s) => s.includes('"t":"send"'));
    expect(send).toBeTruthy();
    expect(JSON.parse(send!)).toMatchObject({
      t: 'send',
      ch: 'transcript:jevons',
      body: { text: 'cut in', mode: 'interrupt', interrupt: true },
    });
    client.interruptTranscript('jevons');
    const cancel = ws.sent.find((s) => s.includes('"t":"interrupt"'));
    expect(JSON.parse(cancel!)).toMatchObject({
      t: 'interrupt',
      ch: 'transcript:jevons',
    });
    client.close();
  });
});

describe('MuxClient heartbeat (T537.2.1)', () => {
  it('sends the vanilla chat ping on open and every heartbeat interval', () => {
    const { client, ws } = connectClient();
    expect(ws.sent).toEqual([CHAT_PING]);
    expect(CHAT_PING).toBe('{"type":"ping"}');
    expect(HEARTBEAT_MS).toBe(15000);

    vi.advanceTimersByTime(HEARTBEAT_MS - 1);
    expect(ws.sent).toEqual([CHAT_PING]);
    vi.advanceTimersByTime(1);
    expect(ws.sent).toEqual([CHAT_PING, CHAT_PING]);
    vi.advanceTimersByTime(HEARTBEAT_MS);
    expect(ws.sent).toEqual([CHAT_PING, CHAT_PING, CHAT_PING]);

    client.close();
    vi.advanceTimersByTime(HEARTBEAT_MS * 2);
    expect(ws.sent).toHaveLength(3);
  });

  it('swallows pong and does not dispatch it as a mux envelope', () => {
    const { client, ws } = connectClient();
    const seen: string[] = [];
    client.subscribe('*', (env) => seen.push(env.t));
    ws.onmessage?.({ data: '{"type":"pong"}' });
    expect(seen).toEqual([]);
    client.close();
  });
});
