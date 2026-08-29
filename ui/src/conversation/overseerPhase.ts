// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Closed overseer turn-state (🎯T555). Wire names vs status-bar copy. */

export const PHASE_IDLE = 'idle';
export const PHASE_ACCEPTED = 'accepted';
export const PHASE_THINKING = 'thinking';
export const PHASE_TOOL = 'tool';
export const PHASE_STREAMING = 'streaming';
export const PHASE_PERMISSION = 'permission';
export const PHASE_ERROR = 'error';
export const PHASE_STUCK = 'stuck';

const PHASE_COPY: Record<string, string> = {
  [PHASE_IDLE]: 'idle',
  [PHASE_ACCEPTED]: 'received',
  [PHASE_THINKING]: 'thinking',
  [PHASE_TOOL]: 'tool',
  [PHASE_STREAMING]: 'writing',
  [PHASE_PERMISSION]: 'permission',
  [PHASE_ERROR]: 'error',
  [PHASE_STUCK]: 'stuck',
};

export type OverseerPhaseSample = {
  phase: string;
  step?: string;
  tokens?: number;
  correspondent?: string[];
};

function rec(v: unknown): Record<string, unknown> {
  return v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : {};
}

function asStringList(v: unknown): string[] {
  if (!Array.isArray(v)) return [];
  return v.map((x) => String(x || '').trim()).filter(Boolean);
}

/** Reduce one progress frame or mux/history_meta.phase payload. */
export function phaseSampleFromUnknown(body: unknown): OverseerPhaseSample | null {
  if (typeof body === 'string') {
    const phase = body.trim();
    return phase ? { phase } : null;
  }
  const o = rec(body);
  const nested = o.phase;
  if (nested && typeof nested === 'object' && !Array.isArray(nested)) {
    return phaseSampleFromUnknown(nested);
  }
  const phase = typeof nested === 'string' ? nested.trim() : '';
  if (!phase) return null;
  const step = typeof o.step === 'string' ? o.step.trim() : '';
  const tokens = typeof o.tokens === 'number' && o.tokens > 0 ? o.tokens : 0;
  const correspondent = asStringList(o.correspondent);
  const out: OverseerPhaseSample = { phase };
  if (step) out.step = step;
  if (tokens) out.tokens = tokens;
  if (correspondent.length) out.correspondent = correspondent;
  return out;
}

export function mergePhaseMeta(
  meta: Record<string, unknown> | null | undefined,
  sample: OverseerPhaseSample,
): Record<string, unknown> {
  const next = { ...(meta || {}) };
  next.phase = sample;
  if (sample.step) next.step = sample.step;
  else delete next.step;
  if (sample.tokens) next.tokens = sample.tokens;
  else delete next.tokens;
  if (sample.correspondent && sample.correspondent.length) next.correspondent = sample.correspondent;
  else delete next.correspondent;
  return next;
}

/** Status-bar copy: one phase word, no Jevons-is prefix (🎯T555.2). */
export function formatOverseerStatus(sample: OverseerPhaseSample | null | undefined): string {
  const phase = String(sample?.phase || PHASE_IDLE).trim() || PHASE_IDLE;
  let word = PHASE_COPY[phase] || phase;
  if (phase === PHASE_TOOL && sample?.step) word = sample.step;
  if (phase === PHASE_STREAMING && sample?.tokens && sample.tokens > 0) {
    word = 'writing · ' + String(sample.tokens);
  }
  if (phase !== PHASE_IDLE && sample?.correspondent && sample.correspondent.length) {
    word += ' · ' + sample.correspondent.join(', ');
  }
  return word.replace(/^\s*Jevons\s*(is|:)\s*/i, '').trim() || PHASE_IDLE;
}

/** #status-text: WS connecting vs painted phase, including idle. */
export function statusBarText(
  connected: boolean,
  meta: unknown,
): string {
  if (!connected) return 'connecting';
  return formatOverseerStatus(phaseSampleFromUnknown(meta) || { phase: PHASE_IDLE });
}

export function optimisticReceived(): OverseerPhaseSample {
  return { phase: PHASE_ACCEPTED };
}
