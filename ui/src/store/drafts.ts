// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export const useDrafts = create<{
  drafts: Record<string, string>;
  setDraft: (name: string, text: string) => void;
}>()(
  persist(
    (set) => ({
      drafts: {},
      setDraft: (name, text) =>
        set((s) => ({ drafts: { ...s.drafts, [name]: text } })),
    }),
    { name: 'jevons-drafts' },
  ),
);
