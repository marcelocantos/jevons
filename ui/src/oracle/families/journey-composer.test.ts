// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('journey-composer'), () => {
  itOracle.skip(
    ['T76', 'T123', 'T126', 'T478'],
    'J24 drives Home/End, empty height, and image clipboard paste',
    'journey is the arbiter (J24 live fail) — source-grep of react_paint.js is mention-only',
  );
});
