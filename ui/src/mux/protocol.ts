// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

export const MUX_VERSION = 1;

export type MuxType =
  | 'hello'
  | 'open'
  | 'close'
  | 'frame'
  | 'meta'
  | 'page'
  | 'send'
  | 'error'
  | 'reset';

export type MuxEnvelope = {
  v: number;
  ch: string;
  t: MuxType;
  body?: unknown;
};

export function transcriptChannel(name: string): string {
  return `transcript:${name.trim()}`;
}

export function encodeMux(ch: string, t: MuxType, body?: unknown): string {
  const env: MuxEnvelope = { v: MUX_VERSION, ch, t };
  if (body !== undefined) env.body = body;
  return JSON.stringify(env);
}

export function decodeMux(raw: string): MuxEnvelope | null {
  try {
    const env = JSON.parse(raw) as MuxEnvelope;
    if (!env || env.v !== MUX_VERSION || typeof env.t !== 'string') return null;
    return env;
  } catch {
    return null;
  }
}
