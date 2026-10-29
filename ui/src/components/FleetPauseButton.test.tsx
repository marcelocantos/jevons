// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { FleetPauseButton } from './FleetPauseButton';

afterEach(cleanup);

it('🎯T966: pause button sits as play while paused and calls resume', async () => {
  const onToggle = vi.fn(async () => {});
  render(<FleetPauseButton paused={true} onToggle={onToggle} />);
  const btn = screen.getByRole('button', { name: 'Resume fleet' });
  expect(btn.textContent).toBe('▶');
  fireEvent.click(btn);
  expect(onToggle).toHaveBeenCalledWith(false);
});

it('🎯T966: running fleet shows pause icon', () => {
  render(<FleetPauseButton paused={false} onToggle={async () => {}} />);
  expect(screen.getByRole('button', { name: 'Pause fleet' }).textContent).toBe('❚❚');
});
