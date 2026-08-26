// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('journey-ticker'), () => {
  itOracle.skip(
    ['T175', 'T390', 'T248'],
    'J28 hovers #plan-ticker InstantTip (remaining/rollover), not title=',
    'journey is the arbiter (J28 live fail) — source-grep of react_paint.js is mention-only',
  );
});
