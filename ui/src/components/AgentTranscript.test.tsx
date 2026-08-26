// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createElement } from 'react';
import { render } from '@testing-library/react';
import { expect, it } from 'vitest';
import { userTurn } from '../oracle/fixtures';
import { paintUserHTML } from '../conversation/paint';
import { AgentTranscript } from './AgentTranscript';

const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'AgentTranscript.tsx'), 'utf8');

it('UserBody paints owner text without a renderUserTextWithImages crash (T540.3.1)', () => {
  expect(src).toMatch(/paintUserHTML\(props\.text, props\.origin\)/);
  expect(src).not.toMatch(/renderUserTextWithImages\(/);
  expect(paintUserHTML('hello from the UserBody paint path', 'owner')).toContain(
    'hello from the UserBody paint path',
  );
  const { container } = render(
    createElement(AgentTranscript, {
      name: 'jevons',
      frames: [userTurn('hello from the UserBody paint path')],
      meta: { start: 0, older: 0, total: 1 },
      ready: true,
    }),
  );
  expect(container.querySelector('#messages')).toBeTruthy();
});
