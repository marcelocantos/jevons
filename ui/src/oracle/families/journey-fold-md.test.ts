// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('journey-fold-md'), () => {
  itOracle.skip(
    ['T59', 'T106', 'T238'],
    'J23 asserts mermaid svg, .msg-clipped pocket, and no [silent] owner bubble',
    'journey is the arbiter (J23 live fail) — source-grep of react_paint.js is mention-only',
  );
});
