// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('journey-aside'), () => {
  itOracle.skip(
    ['T95', 'T250', 'T251'],
    'J26 sends target: and forbids a main .msg.user leak',
    'journey is the arbiter (J26 live fail) — source-grep of react_paint.js is mention-only',
  );
});
