// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T266/T267: ambient inputs the transcript bubble needs to paint the
// target-context tab (fleet agents, selected agent workdir) and the hook a
// live target-ask fires so the host can select the owning PO + highlight.

import { createContext, useContext } from 'react';
import type { ContextAgent } from './targetAsk';

export type TargetAskHost = {
  agents: ContextAgent[];
  selectedAgent: string;
  /** Fired once per live sealed Jevons bubble (never during history replay). */
  onTargetAsk?: (text: string) => void;
};

export const TargetAskContext = createContext<TargetAskHost>({ agents: [], selectedAgent: '' });

export function useTargetAskHost(): TargetAskHost {
  return useContext(TargetAskContext);
}

/** Workdir of the selected agent, the React stand-in for vanilla's ledger/cwd cache. */
export function selectedWorkdir(host: TargetAskHost): string {
  const row = host.agents.find((a) => a && a.name === host.selectedAgent);
  return row && row.workdir ? String(row.workdir) : '';
}
