// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useRef } from 'react';
import { themeLabel, type ThemePref } from '../theme';

type Props = {
  open: boolean;
  onClose: () => void;
  connected: boolean;
  theme: ThemePref;
  onTheme: (pref: ThemePref) => void;
  onResetLayout: () => void;
};

/** Browser settings are same-origin: the connection address is the page origin,
 * not a separately configurable native-app server URL. */
export function CockpitSettings({ open, onClose, connected, theme, onTheme, onResetLayout }: Props) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const el = dialog.current;
    if (!el) return;
    if (open && !el.open) el.showModal();
    if (!open && el.open) el.close();
  }, [open]);

  return (
    <dialog ref={dialog} id="cockpit-settings" aria-labelledby="cockpit-settings-title" onClose={onClose}
      onClick={(e) => { if (e.target === dialog.current) dialog.current.close(); }}>
      <div className="settings-content">
        <header>
          <h2 id="cockpit-settings-title">Settings</h2>
          <button type="button" aria-label="Close settings" onClick={onClose}>✕</button>
        </header>
        <section>
          <h3>Connection</h3>
          <p>Web cockpit connects to the server serving this page.</p>
          <p><strong>URL</strong> <code>{typeof window !== 'undefined' ? window.location.origin : ''}</code></p>
          <p role="status">{connected ? 'Connected' : 'Disconnected'}</p>
        </section>
        <section>
          <h3>Preferences</h3>
          <label htmlFor="settings-theme">Theme</label>
          <select id="settings-theme" value={theme} onChange={(e) => onTheme(e.target.value as ThemePref)}>
            {(['system', 'light', 'dark'] as const).map((pref) =>
              <option key={pref} value={pref}>{themeLabel(pref)}</option>) }
          </select>
          <button type="button" onClick={onResetLayout}>Reset sidebar layout</button>
        </section>
      </div>
    </dialog>
  );
}
