// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('journey-send'), () => {
  itOracle.skip(
    ['T279', 'T281', 'T504'],
    'J22 asserts one .msg.user per send and the reply sits below it',
    'journey is the arbiter (J22 live fail) — source-grep of react_paint.js is mention-only',
  );
});
