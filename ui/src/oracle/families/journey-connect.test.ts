// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('journey-connect'), () => {
  itOracle.skip(
    ['T491', 'T493', 'T494'],
    'J19 remains the connect-tail journey (retarget at React)',
    'journey is the arbiter (J19 live paint) — source-grep of j19_root_history.go is mention-only',
  );
  itOracle.skip('T494', 'J19 hard-load paints the React replay tail (isolate GET / or :5173)', 'journey is the arbiter (J19 live paint)');
});
