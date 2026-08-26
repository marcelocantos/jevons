// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createContext, useContext } from 'react';
import type { FrontierRow } from './table';

/** Chat 🎯Tn hotspots share the frontier card cache (🎯T326). */
export const FrontierRowsContext = createContext<readonly FrontierRow[]>([]);

export function useFrontierRows(): readonly FrontierRow[] {
  return useContext(FrontierRowsContext);
}
