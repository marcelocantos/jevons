// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { pageScrollDelta } from '../../keys/pageScroll';
import { pinWriteScrollTop } from '../../transcript/followPin';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('transcript-geom'), () => {
  itOracle('T336', 'PageUp/PageDown step is ~0.8 of the viewport', () => {
    expect(pageScrollDelta('PageUp', 1000)).toBe(-800);
    expect(pageScrollDelta('PageDown', 1000)).toBe(800);
    expect(pageScrollDelta('Home', 1000)).toBe(0);
  });

  itOracle('T351', 'pin write uses full scrollHeight so the browser clamps the fractional max', () => {
    expect(pinWriteScrollTop(1234.7)).toBe(1234.7);
    expect(pinWriteScrollTop(0)).toBe(0);
  });

  itOracle.skip('T56', 'only on-screen messages are in the DOM', 'census exception: React owns reconciliation');
  itOracle.skip('T30.2', 'in-flight follow-scroll stays pinned to the true bottom until seal', 'journey is the arbiter (J19)');
  itOracle.skip('T119', 'history is windowed, recent-first, whole-chunk', 'journey is the arbiter (J19)');
  itOracle.skip('T119.1', 'reload/reconnect does not scroll-parade to the live end', 'journey is the arbiter (J19)');
  itOracle.skip('T119.3', 'absolute-position virtual list: O(viewport) nodes', 'census exception family: React owns reconciliation');
  itOracle.skip('T341', 'main chat text does not jiggle from pin/reflow thrash', 'named residual: pixel-identical chrome');
  itOracle.skip('T347', 'reload paints end-first and only materializes viewport plus a few rows', 'journey is the arbiter (J19)');
  itOracle.skip('T363', 'scroll-up preserves viewport when older content prepends', 'journey is the arbiter (J19)');
  itOracle.skip('T491', 'connect replay is one virtual-list row per owner turn', 'journey is the arbiter (J19)');
  itOracle.skip('T494', 'connect shows the replay tail in the viewport, not an empty pane', 'journey is the arbiter (J19)');
  itOracle.skip('T494.1.2', 'layout is a function of transcript and width, not scroll history', 'named residual: pixel-identical chrome');
});
