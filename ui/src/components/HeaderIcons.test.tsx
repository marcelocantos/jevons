// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { SettingsIcon, ThemeIcon } from './HeaderIcons';

describe('header icons', () => {
  it('renders a decorative vector settings icon', () => {
    const markup = renderToStaticMarkup(<SettingsIcon />);
    expect(markup).toContain('<svg');
    expect(markup).toContain('aria-hidden="true"');
    expect(markup).toContain('class="header-icon"');
    expect(markup).not.toContain('⚙');
  });

  it('renders distinct decorative vector icons for each theme', () => {
    const icons = ['light', 'system', 'dark'].map((theme) => renderToStaticMarkup(<ThemeIcon theme={theme as 'light' | 'system' | 'dark'} />));
    expect(new Set(icons).size).toBe(3);
    for (const icon of icons) {
      expect(icon).toContain('<svg');
      expect(icon).toContain('aria-hidden="true"');
      expect(icon).toContain('class="header-icon"');
    }
  });
});
