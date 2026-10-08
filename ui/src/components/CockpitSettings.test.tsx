// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// @vitest-environment jsdom
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { CockpitSettings } from './CockpitSettings';

beforeEach(() => {
  HTMLDialogElement.prototype.showModal = function () { this.setAttribute('open', ''); };
  HTMLDialogElement.prototype.close = function () { this.removeAttribute('open'); this.dispatchEvent(new Event('close')); };
});
afterEach(() => cleanup());

test('explicit settings dialog exposes same-origin connection, theme, layout and close', () => {
  const onClose = vi.fn();
  const onTheme = vi.fn();
  const onResetLayout = vi.fn();
  const { rerender } = render(<CockpitSettings open connected theme="system" onClose={onClose}
    onTheme={onTheme} onResetLayout={onResetLayout} />);
  expect(screen.getByRole('dialog', { name: 'Settings' })).toBeTruthy();
  expect(screen.getByText(window.location.origin)).toBeTruthy();
  expect(screen.getByRole('status').textContent).toBe('Connected');
  fireEvent.change(screen.getByLabelText('Theme'), { target: { value: 'dark' } });
  expect(onTheme).toHaveBeenCalledWith('dark');
  fireEvent.click(screen.getByRole('button', { name: 'Reset sidebar layout' }));
  expect(onResetLayout).toHaveBeenCalledOnce();
  rerender(<CockpitSettings open connected={false} theme="dark" onClose={onClose}
    onTheme={onTheme} onResetLayout={onResetLayout} />);
  expect(screen.getByRole('status').textContent).toBe('Disconnected');
  fireEvent.click(screen.getByRole('button', { name: 'Close settings' }));
  expect(onClose).toHaveBeenCalledOnce();
});

test('cockpit status bar exposes the dialog with an explicit button', async () => {
  const { readFileSync } = await import('node:fs');
  const { fileURLToPath } = await import('node:url');
  const { dirname, join } = await import('node:path');
  const app = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../App.tsx'), 'utf8');
  expect(app).toMatch(/id="settings-button"[^>]*aria-label="Open settings"/);
  // The gear is an SVG icon component (4a5558db), not a text glyph.
  expect(app).toMatch(/aria-label="Open settings"[\s\S]*?<SettingsIcon \/>\s*<\/button>/);
  expect(app).toContain('onClick={() => setSettingsOpen(true)}');
  expect(app).toContain('<CockpitSettings open={settingsOpen}');
});
