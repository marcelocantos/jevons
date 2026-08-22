// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { decodeMux, encodeMux, transcriptChannel, type MuxEnvelope } from './protocol';

export type MuxHandler = (env: MuxEnvelope) => void;

export class MuxClient {
  private ws: WebSocket | null = null;
  private readonly handlers = new Map<string, Set<MuxHandler>>();
  private readonly pending: string[] = [];
  private readonly watched = new Set<string>();
  private generation = 0;
  private reconnectTimer = 0;
  private everOpened = false;
  private readonly url: string;

  constructor(url: string) {
    this.url = url;
  }

  connect(): void {
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
      return;
    }
    const ws = new WebSocket(this.url);
    this.ws = ws;
    const gen = ++this.generation;
    ws.onopen = () => {
      if (gen !== this.generation) return;
      for (const msg of this.pending.splice(0)) ws.send(msg);
      if (this.everOpened) {
        for (const name of this.watched) {
          this.dispatch({ v: 1, ch: transcriptChannel(name), t: 'reset' });
          ws.send(encodeMux(transcriptChannel(name), 'open'));
        }
      }
      this.everOpened = true;
    };
    ws.onmessage = (ev) => {
      if (typeof ev.data !== 'string') return;
      const env = decodeMux(ev.data);
      if (!env) return;
      this.dispatch(env);
    };
    ws.onclose = () => {
      if (gen !== this.generation) return;
      this.ws = null;
      window.clearTimeout(this.reconnectTimer);
      this.reconnectTimer = window.setTimeout(() => this.connect(), 500);
    };
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

  openTranscript(name: string): void {
    this.watched.add(name);
    this.send(encodeMux(transcriptChannel(name), 'open'));
  }

  closeTranscript(name: string): void {
    this.watched.delete(name);
    this.send(encodeMux(transcriptChannel(name), 'close'));
  }

  pageTranscript(name: string, end: number, limit: number): void {
    this.send(encodeMux(transcriptChannel(name), 'page', { end, limit }));
  }

  sendTranscript(name: string, text: string): void {
    this.send(encodeMux(transcriptChannel(name), 'send', { text }));
  }

  private send(raw: string): void {
    this.connect();
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(raw);
      return;
    }
    this.pending.push(raw);
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
