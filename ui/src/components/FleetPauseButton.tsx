// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';

export function FleetPauseButton(props: { paused: boolean; onToggle: (pause: boolean) => Promise<void> }) {
  const [busy, setBusy] = useState(false);
  const paused = props.paused;
  return (
    <button
      id="fleet-pause-btn"
      type="button"
      className={'fleet-pause-btn' + (paused ? ' fleet-pause-btn-active' : '')}
      aria-label={paused ? 'Resume fleet' : 'Pause fleet'}
      title={paused ? 'Resume fleet — allow new model and tool work' : 'Pause fleet — freeze token spend (seats stay alive)'}
      disabled={busy}
      onClick={() => {
        if (busy) return;
        setBusy(true);
        void props.onToggle(!paused).finally(() => setBusy(false));
      }}
    >
      {paused ? '▶' : '❚❚'}
    </button>
  );
}
