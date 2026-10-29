// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useQuery, useQueryClient } from '@tanstack/react-query';

export function fleetPausedFromIntent(body: unknown): boolean {
  if (!body || typeof body !== 'object') return false;
  const fleetState = (body as { fleet_state?: string }).fleet_state;
  return fleetState === 'blocked_owner';
}

export function useFleetPaused() {
  const queryClient = useQueryClient();
  const q = useQuery({
    queryKey: ['fleet-intent'],
    queryFn: async () => {
      const r = await fetch('/api/fleet-intent');
      if (!r.ok) throw new Error('fleet-intent HTTP ' + r.status);
      return (await r.json()) as Record<string, unknown>;
    },
    staleTime: 2_000,
    refetchInterval: 3_000,
  });
  const paused = fleetPausedFromIntent(q.data);
  const toggle = async (next: boolean) => {
    const r = await fetch('/api/fleet-pause', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pause: next, reason: next ? 'owner cockpit pause' : 'owner cockpit resume' }),
    });
    if (!r.ok) {
      throw new Error((await r.text()) || 'fleet-pause HTTP ' + r.status);
    }
    await queryClient.invalidateQueries({ queryKey: ['fleet-intent'] });
  };
  return { paused, loading: q.isLoading && !q.data, toggle };
}
