// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { deliveryModeOf, type DeliveryMode } from '../composer/deliveryMode';
import { decodeMux, encodeMux, transcriptChannel, type MuxEnvelope } from './protocol';

export type MuxHandler = (env: MuxEnvelope) => void;

/** Same payload vanilla web/scripts/transport.js sends on /ws/chat (🎯T537.2.1). */
export const CHAT_PING = '{"type":"ping"}';
export const HEARTBEAT_MS = 15000;
/** Reconnect backoff (🎯T798): base * 2^attempt, capped, scaled by jitter in [0.75, 1]. */
export const RECONNECT_BASE_MS = 500;
export const RECONNECT_CAP_MS = 10000;
const JITTER_FLOOR = 0.75;

/** Delay before reconnect attempt `attempt` (0-based). Jitter keeps the ranges of consecutive attempts disjoint until the cap. */
export function reconnectDelayMs(attempt: number, rand: () => number = Math.random): number {
  const exp = Math.min(RECONNECT_CAP_MS, RECONNECT_BASE_MS * 2 ** attempt);
  return Math.round(exp * (JITTER_FLOOR + (1 - JITTER_FLOOR) * rand()));
}

/**
 * 🎯T993: auto-reload on a genuinely new build.
 *
 * The server names its binary in the hello frame that opens every mux
 * connection, and stamps the same id into the served document as
 * <meta name="jevons-build">. A hello only ever arrives on a socket that
 * opened, so comparing there — and nowhere else — is what makes the safety
 * property structural: a disconnect, a refused connect or a server outage
 * never reaches this code, and the existing backoff reconnects in place.
 * A reload fires only when a hello names a build other than the one the
 * page loaded with.
 *
 * The debounce survives the reload (sessionStorage): a page that reloads
 * and immediately sees another mismatch — a flapping server, a build that
 * serves a stale document — holds still for RELOAD_DEBOUNCE_MS instead of
 * spinning. The mobile shell has no reload button; a loop would strand it.
 */
export const BUILD_META_NAME = 'jevons-build';
export const RELOAD_DEBOUNCE_MS = 3 * 60 * 1000;
export const RELOAD_STAMP_KEY = 'jevons.mux.autoReloadAt';

export type ReloadVerdict = 'reload' | 'same' | 'unknown' | 'debounced';

/**
 * Pure decision: should a hello naming `server` reload a page loaded with
 * `loaded`? `lastReloadAt` is the epoch ms of the previous auto-reload (0 =
 * never). Unknown on either side is never a reload: a daemon that predates
 * the id, or cannot read its own binary, must not bounce the client.
 */
export function buildReloadVerdict(
  loaded: string,
  server: string,
  lastReloadAt: number,
  now: number,
): ReloadVerdict {
  if (!loaded || !server) return 'unknown';
  if (loaded === server) return 'same';
  // A clock that went backwards reads as inside the window: holding still
  // is the safe side of the loop guard.
  if (lastReloadAt > 0 && now - lastReloadAt < RELOAD_DEBOUNCE_MS) return 'debounced';
  return 'reload';
}

/** The build id stamped into the document the page loaded from, or '' when the document carries none. */
export function loadedBuildFromDocument(): string {
  if (typeof document === 'undefined') return '';
  const meta = document.querySelector(`meta[name="${BUILD_META_NAME}"]`);
  return (meta?.getAttribute('content') ?? '').trim();
}

type ReloadStampStore = Pick<Storage, 'getItem' | 'setItem'>;

function sessionStampStore(): ReloadStampStore | null {
  try {
    return typeof sessionStorage === 'undefined' ? null : sessionStorage;
  } catch {
    return null; // private mode / blocked storage: the in-memory `reloading` flag still prevents a same-page loop
  }
}

export type MuxClientOptions = {
  /** Build the page loaded with. Default: the document's meta. '' adopts the first hello's id as the baseline. */
  loadedBuild?: string;
  /** Performs the reload. Default: window.location.reload(). */
  reload?: () => void;
  /** Clock for the debounce. Default: Date.now. */
  now?: () => number;
  /** Where the last-reload stamp lives across reloads. Default: sessionStorage; null disables persistence. */
  stamps?: ReloadStampStore | null;
};

export class MuxClient {
  private ws: WebSocket | null = null;
  private readonly handlers = new Map<string, Set<MuxHandler>>();
  private readonly pending: string[] = [];
  private readonly watched = new Set<string>();
  private readonly watchRefs = new Map<string, number>();
  private readonly channels = new Set<string>();
  private generation = 0;
  private reconnectTimer = 0;
  private heartbeatTimer = 0;
  private everOpened = false;
  private closed = false;
  private attempts = 0;
  private outageLogged = false;
  private sendSeq = 0;
  private readonly url: string;
  private readonly rand: () => number;
  // 🎯T993 build-id reload state. `loadedBuild` is '' until known;
  // `reloading` is set the instant a reload is requested and never cleared,
  // so one page requests at most one reload whatever hellos follow.
  private loadedBuild: string;
  private reloading = false;
  private readonly reload: () => void;
  private readonly now: () => number;
  private readonly stamps: ReloadStampStore | null;
  onOpen?: () => void;
  onClose?: () => void;

  constructor(url: string, rand: () => number = Math.random, opts: MuxClientOptions = {}) {
    this.url = url;
    this.rand = rand;
    this.loadedBuild = (opts.loadedBuild ?? loadedBuildFromDocument()).trim();
    this.reload = opts.reload ?? (() => window.location.reload());
    this.now = opts.now ?? Date.now;
    this.stamps = opts.stamps === undefined ? sessionStampStore() : opts.stamps;
  }

  /** The build this page considers itself loaded with ('' = not yet known). */
  get build(): string {
    return this.loadedBuild;
  }

  connect(): void {
    if (this.closed) return;
    // A backoff timer is pending: sends queue in `pending` and ride the next attempt.
    if (this.reconnectTimer && !this.ws) return;
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
      return;
    }
    this.reconnectTimer = 0;
    const ws = new WebSocket(this.url);
    this.ws = ws;
    const gen = ++this.generation;
    ws.onopen = () => {
      if (gen !== this.generation) return;
      this.attempts = 0;
      this.outageLogged = false;
      for (const msg of this.pending.splice(0)) ws.send(msg);
      if (this.everOpened) {
        for (const name of this.watched) {
          this.dispatch({ v: 1, ch: transcriptChannel(name), t: 'reset' });
          ws.send(encodeMux(transcriptChannel(name), 'open', { lo: -30, hi: 0 }));
        }
        for (const ch of this.channels) {
          this.dispatch({ v: 1, ch, t: 'reset' });
          ws.send(encodeMux(ch, 'open'));
        }
      }
      this.everOpened = true;
      this.startHeartbeat();
      this.onOpen?.();
    };
    ws.onmessage = (ev) => {
      if (typeof ev.data !== 'string') return;
      if (ev.data === '{"type":"pong"}') return;
      const env = decodeMux(ev.data);
      if (!env) return;
      if (env.t === 'hello' && env.ch === '') this.noteServerBuild(env);
      this.dispatch(env);
    };
    ws.onclose = () => {
      if (gen !== this.generation) return;
      this.stopHeartbeat();
      this.ws = null;
      this.onClose?.();
      if (this.closed) return;
      clearTimeout(this.reconnectTimer);
      if (!this.outageLogged) {
        this.outageLogged = true;
        console.error('mux: /ws/mux connection lost; retrying with backoff');
      }
      const delay = reconnectDelayMs(this.attempts++, this.rand);
      this.reconnectTimer = setTimeout(() => {
        this.reconnectTimer = 0;
        this.connect();
      }, delay);
    };
  }

  /** Stop reconnect and heartbeat. Tests and a tab unload use this. */
  close(): void {
    this.closed = true;
    this.generation += 1;
    this.stopHeartbeat();
    clearTimeout(this.reconnectTimer);
    this.reconnectTimer = 0;
    const ws = this.ws;
    this.ws = null;
    if (ws) {
      ws.onopen = null;
      ws.onmessage = null;
      ws.onclose = null;
      try {
        ws.close();
      } catch {
        /* ignore */
      }
    }
  }

  subscribe(ch: string, handler: MuxHandler): () => void {
    let set = this.handlers.get(ch);
    if (!set) {
      set = new Set();
      this.handlers.set(ch, set);
    }
    set.add(handler);
    return () => {
      set!.delete(handler);
      if (set!.size === 0) this.handlers.delete(ch);
    };
  }

  openTranscript(name: string, win?: { lo: number; hi: number }): void {
    const n = (this.watchRefs.get(name) || 0) + 1;
    this.watchRefs.set(name, n);
    this.watched.add(name);
    if (n === 1) {
      this.send(encodeMux(transcriptChannel(name), 'open', win ?? { lo: -30, hi: 0 }));
    }
  }

  closeTranscript(name: string): void {
    const n = (this.watchRefs.get(name) || 1) - 1;
    if (n > 0) {
      this.watchRefs.set(name, n);
      return;
    }
    this.watchRefs.delete(name);
    this.watched.delete(name);
    this.send(encodeMux(transcriptChannel(name), 'close'));
  }

  windowTranscript(name: string, win: { lo: number; hi: number }): void {
    this.send(encodeMux(transcriptChannel(name), 'window', win));
  }

  pageTranscript(
    name: string,
    spec: number | { before?: string; limit?: number; end?: number },
    limit?: number,
  ): void {
    const body =
      typeof spec === 'number' ? { end: spec, limit: limit ?? 50 } : spec;
    this.send(encodeMux(transcriptChannel(name), 'page', body));
  }

  /**
   * 🎯T562.5: sendId correlates the reply (status = ack, error = definite
   * failure) to THIS send, not merely "some send happened" — two rapid
   * sends on the same channel must not have the second's ack satisfy the
   * first's caller. Returns the id so the caller can match the outcome.
   */
  sendTranscript(
    name: string,
    text: string,
    opts?: { mode?: DeliveryMode; interrupt?: boolean },
  ): string {
    // 🎯T657: mode is the wire; interrupt=true rides along as the deprecated
    // alias so a daemon that predates send.mode still cancels the turn.
    const mode = deliveryModeOf(opts);
    const id = 's' + (++this.sendSeq) + '-' + Math.round(this.rand() * 1e9).toString(36);
    const body: { text: string; mode?: DeliveryMode; interrupt?: boolean; id: string } = { text, id };
    if (mode !== 'submit') body.mode = mode;
    if (mode === 'interrupt') body.interrupt = true;
    this.send(encodeMux(transcriptChannel(name), 'send', body));
    return id;
  }

  /** Retry a queued, undelivered owner message now; never re-sends the text (🎯T811). */
  resendTranscript(name: string, msgId: string): void {
    this.send(encodeMux(transcriptChannel(name), 'resend', { msg_id: msgId }));
  }

  /** Empty Cmd+Enter: cancel the in-flight turn without sending (🎯T644). */
  interruptTranscript(name: string): void {
    this.send(encodeMux(transcriptChannel(name), 'interrupt'));
  }

  /** Open a non-transcript snapshot channel (plan-usage, 🎯T631). Re-opens on reconnect. */
  openChannel(ch: string): void {
    const name = ch.trim();
    if (!name) return;
    this.channels.add(name);
    this.send(encodeMux(name, 'open'));
  }

  closeChannel(ch: string): void {
    const name = ch.trim();
    if (!name) return;
    this.channels.delete(name);
    this.send(encodeMux(name, 'close'));
  }

  private send(raw: string): void {
    this.connect();
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(raw);
      return;
    }
    this.pending.push(raw);
  }

  private startHeartbeat(): void {
    this.stopHeartbeat();
    this.sendPing();
    this.heartbeatTimer = setInterval(() => this.sendPing(), HEARTBEAT_MS);
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = 0;
    }
  }

  private sendPing(): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    try {
      this.ws.send(CHAT_PING);
    } catch {
      /* ignore: next interval retries */
    }
  }

  /**
   * 🎯T993: runs only for the hello that opens a connection, i.e. only after
   * a successful (re)connect. Never called from onclose or a failed attempt.
   */
  private noteServerBuild(env: MuxEnvelope): void {
    const body = env.body as { build?: unknown } | undefined;
    const server = typeof body?.build === 'string' ? body.build.trim() : '';
    if (!server) return;
    if (!this.loadedBuild) {
      // No meta in the document (Vite dev, an older daemon's document): the
      // first build we ever see is the one we are running.
      this.loadedBuild = server;
      return;
    }
    if (this.reloading) return;
    const now = this.now();
    const verdict = buildReloadVerdict(this.loadedBuild, server, this.readReloadStamp(), now);
    if (verdict === 'debounced') {
      console.warn(`mux: server build ${server} differs from loaded ${this.loadedBuild}; reload held (last auto-reload within ${RELOAD_DEBOUNCE_MS}ms)`);
      return;
    }
    if (verdict !== 'reload') return;
    this.reloading = true;
    this.writeReloadStamp(now);
    console.warn(`mux: server build ${server} differs from loaded ${this.loadedBuild}; reloading`);
    this.reload();
  }

  private readReloadStamp(): number {
    try {
      const raw = this.stamps?.getItem(RELOAD_STAMP_KEY);
      const t = raw ? Number(raw) : 0;
      return Number.isFinite(t) && t > 0 ? t : 0;
    } catch {
      return 0;
    }
  }

  private writeReloadStamp(t: number): void {
    try {
      this.stamps?.setItem(RELOAD_STAMP_KEY, String(t));
    } catch {
      /* storage blocked: the in-memory flag still holds this page */
    }
  }

  private dispatch(env: MuxEnvelope): void {
    const exact = this.handlers.get(env.ch);
    if (exact) for (const h of exact) h(env);
    const star = this.handlers.get('*');
    if (star) for (const h of star) h(env);
  }
}

export function muxUrl(): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${proto}//${location.host}/ws/mux`;
}
