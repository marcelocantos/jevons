// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect, it } from 'vitest';
import { fleetPausedFromIntent } from './useFleetPaused';

it('reads blocked_owner from fleet-intent JSON', () => {
  expect(fleetPausedFromIntent({ fleet_state: 'blocked_owner' })).toBe(true);
  expect(fleetPausedFromIntent({ fleet_state: 'working' })).toBe(false);
});
