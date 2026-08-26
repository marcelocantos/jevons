// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('journey-fleet'), () => {
  itOracle.skip(
    ['T68', 'T72', 'T72.1'],
    'J25 mints a live agent and requires it in the vanilla #agents tree',
    'journey is the arbiter (J25 live fail) — source-grep of react_paint.js is mention-only',
  );
});
