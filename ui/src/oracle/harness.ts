// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, it } from 'vitest';
import type { OracleFamily } from './catalog';

/**
 * describeOracle / itOracle — one theme for every family file.
 * Workers do not invent a second runner.
 */
export function describeOracle(fam: OracleFamily, fn: () => void): void {
  describe(`oracle:${fam.id} (${fam.layer})`, fn);
}

export function itOracle(covers: string | readonly string[], title: string, fn: () => void): void {
  const ids = typeof covers === 'string' ? covers : covers.join(',');
  it(`${ids}: ${title}`, fn);
}

itOracle.todo = (covers: string | readonly string[], title: string): void => {
  const ids = typeof covers === 'string' ? covers : covers.join(',');
  it.todo(`${ids}: ${title}`);
};

itOracle.skip = (covers: string | readonly string[], title: string, reason: string): void => {
  const ids = typeof covers === 'string' ? covers : covers.join(',');
  it.skip(`${ids}: ${title} — ${reason}`, () => {});
};
