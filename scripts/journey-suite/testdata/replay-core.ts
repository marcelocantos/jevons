// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
// 🎯T1300: deterministic, fail-closed model-turn cassette protocol.
import { createHash } from "node:crypto";

export type Entry = { seat: string; hash: string; output: unknown };
export type Cassette = { version: 1; journey: "j50" | "j51"; source_hash: string; entries: Entry[] };
export type Paths = { workdir: string; state: string; gateDir: string; authoredTargetIds: string[] };
const sha256 = (text: string) => createHash("sha256").update(text).digest("hex");

// Workdir/state are substituted longest-first: gateDir is often inside state.
// Exact authored target ids are protected, not all ids; allocating a new target
// is nondeterministic but an edit to the journey's own brief is not.
export function canonicalize(text: string, paths: Paths): string {
  let out = text;
  for (const [value, token] of [
    [paths.gateDir, "${GATE_DIR}"], [paths.workdir, "${WORKDIR}"], [paths.state, "${STATE}"],
  ].sort((a, b) => b[0].length - a[0].length)) {
    if (value) out = out.replaceAll(value, token);
  }
  out = out.replace(/^\s*STATE DIR:.*$/gm, "STATE DIR: ${STATE}");
  out = out.replace(/\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/gi, "${UUID}");
  out = out.replace(/\b\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)\b/g, "${TS}");
  out = out.replace(/\bid=[0-9a-f]{8}\b/gi, "id=${GATE}");
  out = out.replace(/\b[0-9a-f]{40}\b/gi, "${SHA}");
  out = out.replace(/\bT\d+(?:\.\d+)*\b/g, id => paths.authoredTargetIds.includes(id) ? id : "${TID}");
  return out;
}

export function turnHash(context: unknown, paths: Paths): string {
  return sha256(canonicalize(JSON.stringify(context), paths));
}

export class Replay {
  private offsets = new Map<string, number>();
  constructor(readonly cassette: Cassette, sourceHash: string) {
    if (cassette.version !== 1 || cassette.source_hash !== sourceHash) {
      throw new Error(`T1300 stale cassette: source_hash mismatch for ${cassette.journey}`);
    }
  }
  next(seat: string, context: unknown, paths: Paths): unknown {
    const index = this.offsets.get(seat) ?? 0;
    const entries = this.cassette.entries.filter(e => e.seat === seat);
    const entry = entries[index];
    const hash = turnHash(context, paths);
    if (!entry || entry.hash !== hash) {
      throw new Error(`T1300 stale cassette: ${seat} turn ${index} ${entry ? `prompt mismatch got=${hash} want=${entry.hash}` : "extra model call (cassette exhausted)"}`);
    }
    this.offsets.set(seat, index + 1);
    return entry.output;
  }
  finish(): void {
    for (const seat of new Set(this.cassette.entries.map(e => e.seat))) {
      const expected = this.cassette.entries.filter(e => e.seat === seat).length;
      if ((this.offsets.get(seat) ?? 0) !== expected) {
        throw new Error(`T1300 stale cassette: ${seat} unused entries`);
      }
    }
  }
}
