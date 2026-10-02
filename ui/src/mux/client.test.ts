// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  BUILD_META_NAME,
  buildReloadVerdict,
  CHAT_PING,
  HEARTBEAT_MS,
  loadedBuildFromDocument,
  MuxClient,
  type MuxClientOptions,
  RECONNECT_CAP_MS,
  RELOAD_DEBOUNCE_MS,
  RELOAD_STAMP_KEY,
  reconnectDelayMs,
} from './client';
import { encodeMux } from './protocol';

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
    vi.advanceTimersByTime(RECONNECT_CAP_MS);
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

describe('MuxClient resend (T811)', () => {
  it('resendTranscript names the message id and never carries the text', () => {
    const { client, ws } = connectClient();
    client.resendTranscript('jevons', 'om-1');
    const sent = ws.sent.find((s) => s.includes('"t":"resend"'));
    expect(JSON.parse(sent!)).toMatchObject({ t: 'resend', ch: 'transcript:jevons', body: { msg_id: 'om-1' } });
    expect(sent).not.toContain('"text"');
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

describe('MuxClient reconnect backoff (T798)', () => {
  it('delays strictly increase to the cap under worst and best jitter', () => {
    for (const rand of [() => 0, () => 0.999]) {
      const delays = Array.from({ length: 12 }, (_, n) => reconnectDelayMs(n, rand));
      for (let i = 1; i < delays.length; i++) {
        expect(delays[i]).toBeGreaterThanOrEqual(delays[i - 1]!);
      }
      // strictly increasing until the exponential term reaches the cap
      for (let i = 1; i < 5; i++) expect(delays[i]).toBeGreaterThan(delays[i - 1]!);
      expect(Math.max(...delays)).toBeLessThanOrEqual(RECONNECT_CAP_MS);
      expect(delays[0]).toBeGreaterThanOrEqual(370);
      expect(delays[0]).toBeLessThanOrEqual(500);
    }
  });

  it('spaces failed opens by growing delays, logs once per outage, resets on open', () => {
    vi.stubGlobal('WebSocket', FakeWebSocket);
    vi.useFakeTimers();
    const errs = vi.spyOn(console, 'error').mockImplementation(() => {});
    const client = new MuxClient('ws://test/ws/mux', () => 0.999);
    client.connect();
    const gaps: number[] = [];
    let last = Date.now();
    for (let i = 0; i < 6; i++) {
      FakeWebSocket.instances[i]!.close();
      const before = FakeWebSocket.instances.length;
      while (FakeWebSocket.instances.length === before) vi.advanceTimersByTime(10);
      gaps.push(Date.now() - last - 0);
      last = Date.now();
    }
    for (let i = 1; i < 5; i++) expect(gaps[i]).toBeGreaterThan(gaps[i - 1]!);
    expect(errs).toHaveBeenCalledTimes(1);
    // a send during the outage does not bypass the backoff
    const n = FakeWebSocket.instances.length;
    client.openChannel('plan-usage');
    expect(FakeWebSocket.instances.length).toBe(n);
    // success resets attempts and the outage log
    FakeWebSocket.instances[n - 1]!.open();
    FakeWebSocket.instances[n - 1]!.close();
    vi.advanceTimersByTime(500);
    expect(FakeWebSocket.instances.length).toBe(n + 1);
    expect(errs).toHaveBeenCalledTimes(2);
    client.close();
    errs.mockRestore();
  });
});

// 🎯T993: auto-reload on a genuinely new build, never on a disconnect.
describe('MuxClient build-id auto-reload (T993)', () => {
  const hello = (build?: string) =>
    encodeMux('', 'hello', build === undefined ? { conn_id: 'c' } : { conn_id: 'c', build });

  class MemoryStamps {
    map = new Map<string, string>();
    getItem(k: string): string | null {
      return this.map.get(k) ?? null;
    }
    setItem(k: string, v: string): void {
      this.map.set(k, v);
    }
  }

  /** A page: a client with a counted reload, under fake timers, with a FakeWebSocket global. */
  function page(opts: MuxClientOptions & { connect?: boolean } = {}) {
    vi.stubGlobal('WebSocket', FakeWebSocket);
    vi.useFakeTimers();
    const reload = vi.fn();
    const client = new MuxClient('ws://test/ws/mux', () => 0.5, {
      loadedBuild: 'A',
      reload,
      now: () => Date.now(),
      stamps: null,
      ...opts,
    });
    if (opts.connect !== false) client.connect();
    return { client, reload };
  }

  /** The socket the client constructed most recently. */
  const latest = () => FakeWebSocket.instances[FakeWebSocket.instances.length - 1]!;

  /** Wait out the backoff until the client constructs its next socket. */
  function awaitReconnect(): FakeWebSocket {
    const before = FakeWebSocket.instances.length;
    vi.advanceTimersByTime(RECONNECT_CAP_MS);
    if (FakeWebSocket.instances.length !== before + 1) {
      throw new Error(`expected one reconnect, sockets ${before} -> ${FakeWebSocket.instances.length}`);
    }
    return latest();
  }

  it('verdict: unknown on either side never reloads; same never reloads; mismatch reloads unless debounced', () => {
    expect(buildReloadVerdict('', 'B', 0, 1000)).toBe('unknown');
    expect(buildReloadVerdict('A', '', 0, 1000)).toBe('unknown');
    expect(buildReloadVerdict('A', 'A', 0, 1000)).toBe('same');
    expect(buildReloadVerdict('A', 'B', 0, 1000)).toBe('reload');
    const now = 10 * RELOAD_DEBOUNCE_MS;
    expect(buildReloadVerdict('A', 'B', now - 1, now)).toBe('debounced');
    expect(buildReloadVerdict('A', 'B', now - RELOAD_DEBOUNCE_MS + 1, now)).toBe('debounced');
    expect(buildReloadVerdict('A', 'B', now - RELOAD_DEBOUNCE_MS, now)).toBe('reload');
    // a clock that went backwards is inside the window, not a licence to reload
    expect(buildReloadVerdict('A', 'B', now + 5000, now)).toBe('debounced');
    expect(RELOAD_DEBOUNCE_MS).toBeGreaterThanOrEqual(60_000);
  });

  it('(a) disconnect and reconnect to the same build never reloads, however often', () => {
    const errs = vi.spyOn(console, 'error').mockImplementation(() => {});
    const { client, reload } = page();
    latest().open();
    latest().onmessage?.({ data: hello('A') });
    expect(reload).not.toHaveBeenCalled();

    // a routine daemon restart: drop, several refused attempts, then back on the same build
    latest().close();
    expect(reload).not.toHaveBeenCalled();
    for (let i = 0; i < 4; i++) {
      awaitReconnect().close(); // connect refused: closes without ever opening
      expect(reload).not.toHaveBeenCalled();
    }
    const back = awaitReconnect();
    back.open();
    back.onmessage?.({ data: hello('A') });
    expect(reload).not.toHaveBeenCalled();
    expect(client.build).toBe('A');

    // and again, to show the first cycle did not arm anything
    back.close();
    const again = awaitReconnect();
    again.open();
    again.onmessage?.({ data: hello('A') });
    expect(reload).not.toHaveBeenCalled();
    client.close();
    errs.mockRestore();
  });

  it('(a) a drop that never comes back reloads nothing and keeps reconnecting in place', () => {
    const errs = vi.spyOn(console, 'error').mockImplementation(() => {});
    const { client, reload } = page();
    latest().open();
    latest().onmessage?.({ data: hello('A') });
    latest().close();
    for (let i = 0; i < 12; i++) awaitReconnect().close();
    expect(reload).not.toHaveBeenCalled();
    expect(FakeWebSocket.instances.length).toBe(1 + 12);
    client.close();
    errs.mockRestore();
  });

  it('(b) reconnect to a new build reloads exactly once, only after the hello', () => {
    const errs = vi.spyOn(console, 'error').mockImplementation(() => {});
    const warns = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { client, reload } = page();
    latest().open();
    latest().onmessage?.({ data: hello('A') });
    latest().close();
    const next = awaitReconnect();
    next.open();
    // open alone is not evidence of a new build
    expect(reload).not.toHaveBeenCalled();
    next.onmessage?.({ data: hello('B') });
    expect(reload).toHaveBeenCalledTimes(1);
    // whatever follows on this page — more hellos, another drop and reconnect — never reloads again
    next.onmessage?.({ data: hello('B') });
    next.onmessage?.({ data: hello('C') });
    next.close();
    const after = awaitReconnect();
    after.open();
    after.onmessage?.({ data: hello('C') });
    expect(reload).toHaveBeenCalledTimes(1);
    expect(warns).toHaveBeenCalledTimes(1);
    client.close();
    errs.mockRestore();
    warns.mockRestore();
  });

  it('(b) a stale document reloads on its very first hello, not only on a reconnect', () => {
    const { client, reload } = page();
    latest().open();
    latest().onmessage?.({ data: hello('B') });
    expect(reload).toHaveBeenCalledTimes(1);
    client.close();
  });

  it('(c) the debounce window holds a repeated mismatch across the reload', () => {
    const warns = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const stamps = new MemoryStamps();
    vi.setSystemTime(new Date(2026, 9, 2, 12, 0, 0));
    const first = page({ stamps });
    latest().open();
    latest().onmessage?.({ data: hello('B') });
    expect(first.reload).toHaveBeenCalledTimes(1);
    const stampedAt = Date.now();
    expect(stamps.getItem(RELOAD_STAMP_KEY)).toBe(String(stampedAt));
    first.client.close();

    // the reloaded page: loaded with B, and the server now flaps to C at once
    vi.advanceTimersByTime(1000);
    FakeWebSocket.instances = [];
    const second = page({ stamps, loadedBuild: 'B' });
    latest().open();
    latest().onmessage?.({ data: hello('C') });
    expect(second.reload).not.toHaveBeenCalled();
    // a drop and a reconnect inside the window still hold
    latest().close();
    const held = awaitReconnect();
    held.open();
    held.onmessage?.({ data: hello('C') });
    expect(second.reload).not.toHaveBeenCalled();
    expect(stamps.getItem(RELOAD_STAMP_KEY)).toBe(String(stampedAt));

    // once the window has passed, the next successful reconnect may reload again — once
    held.close();
    vi.advanceTimersByTime(RELOAD_DEBOUNCE_MS);
    const later = latest();
    later.open();
    later.onmessage?.({ data: hello('C') });
    expect(second.reload).toHaveBeenCalledTimes(1);
    expect(stamps.getItem(RELOAD_STAMP_KEY)).toBe(String(Date.now()));
    second.client.close();
    warns.mockRestore();
  });

  it('(c) the stamp survives in sessionStorage by default', () => {
    try {
      sessionStorage.removeItem(RELOAD_STAMP_KEY);
    } catch {
      return; // environment without storage: nothing to assert
    }
    vi.setSystemTime(new Date(2026, 9, 2, 12, 0, 0));
    const { client, reload } = page({ stamps: undefined });
    latest().open();
    latest().onmessage?.({ data: hello('B') });
    expect(reload).toHaveBeenCalledTimes(1);
    expect(sessionStorage.getItem(RELOAD_STAMP_KEY)).toBe(String(Date.now()));
    client.close();
    sessionStorage.removeItem(RELOAD_STAMP_KEY);
  });

  it('an unknown build on either side never reloads', () => {
    // daemon predates the id, or cannot read its own binary
    const old = page();
    latest().open();
    latest().onmessage?.({ data: hello() });
    latest().onmessage?.({ data: hello('') });
    expect(old.reload).not.toHaveBeenCalled();
    old.client.close();

    // document without the meta: adopt the first hello as the baseline, then compare
    FakeWebSocket.instances = [];
    const errs = vi.spyOn(console, 'error').mockImplementation(() => {});
    const bare = page({ loadedBuild: '' });
    expect(bare.client.build).toBe('');
    latest().open();
    latest().onmessage?.({ data: hello('A') });
    expect(bare.client.build).toBe('A');
    expect(bare.reload).not.toHaveBeenCalled();
    latest().close();
    const same = awaitReconnect();
    same.open();
    same.onmessage?.({ data: hello('A') });
    expect(bare.reload).not.toHaveBeenCalled();
    same.close();
    const fresh = awaitReconnect();
    fresh.open();
    fresh.onmessage?.({ data: hello('B') });
    expect(bare.reload).toHaveBeenCalledTimes(1);
    bare.client.close();
    errs.mockRestore();
  });

  it('only the connection hello is consulted: a hello on another channel is ignored', () => {
    const { client, reload } = page();
    latest().open();
    latest().onmessage?.({ data: encodeMux('transcript:jevons', 'hello', { build: 'B' }) });
    expect(reload).not.toHaveBeenCalled();
    client.close();
  });

  it('reads the loaded build from the document meta', () => {
    const meta = document.createElement('meta');
    meta.setAttribute('name', BUILD_META_NAME);
    meta.setAttribute('content', ' deadbeefcafef00d ');
    document.head.appendChild(meta);
    try {
      expect(loadedBuildFromDocument()).toBe('deadbeefcafef00d');
      vi.stubGlobal('WebSocket', FakeWebSocket);
      expect(new MuxClient('ws://test/ws/mux').build).toBe('deadbeefcafef00d');
    } finally {
      meta.remove();
    }
    expect(loadedBuildFromDocument()).toBe('');
  });
});
