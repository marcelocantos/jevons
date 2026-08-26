// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('journey-frontier'), () => {
  itOracle.skip(
    ['T131', 'T173', 'T175', 'T181', 'T184', 'T185', 'T186', 'T231', 'T271'],
    'J27 drives Frontier hover card plus Graph panel',
    'journey is the arbiter (J27 live fail) — source-grep of react_paint.js is mention-only',
  );
});
