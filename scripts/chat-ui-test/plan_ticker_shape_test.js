// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

'use strict';

const assert = require('assert');
const { compareTickers } = require('./plan_ticker_shape');

function group(over) {
  return Object.assign({
    cls: 'plan-group',
    provider: 'claude',
    company: 'anthropic',
    iconMark: 'claude-splat',
    iconViewBox: '0 0 24 24',
    iconPath: 'M1 2',
    windows: [Object.assign({
      cls: 'plan-win',
      window: 'session',
      pace: '',
      label: 's',
      barAria: 'true',
      triAria: 'true',
      fillTag: 'span',
      triTag: 'span',
      fillWidth: '62%',
      triLeft: '40%',
      hasFill: true,
      hasTri: true,
      hasTrack: true,
      hasBar: true,
    }, (over && over.window) || {})],
    iconTag: 'svg',
  }, over);
}

function dump(g) {
  return { groupCount: 1, groups: [g] };
}

{
  const a = dump(group({ window: { fillWidth: '62%', triLeft: '40%' } }));
  const b = dump(group({ window: { fillWidth: '11%', triLeft: '90%', cls: 'plan-win plan-ahead' } }));
  assert.strictEqual(compareTickers(a, b).ok, true, 'fill, triangle, and pace class may differ');
}

{
  const a = dump(group({}));
  const b = dump(group({ iconMark: 'nope' }));
  assert.strictEqual(compareTickers(a, b).ok, false, 'icon mark must match');
}

{
  const a = dump(group({}));
  const b = dump(group({ window: { label: 'W' } }));
  assert.strictEqual(compareTickers(a, b).ok, false, 'window label must match');
}

{
  const a = dump(group({}));
  const b = dump(group({ window: { hasTri: false, triTag: '', triLeft: '' } }));
  assert.strictEqual(compareTickers(a, b).ok, false, 'missing triangle is a tree fail');
}

{
  const a = dump(group({}));
  const extra = dump(group({ provider: 'grok', company: 'xai' }));
  extra.groupCount = 2;
  extra.groups.push(extra.groups[0]);
  assert.strictEqual(compareTickers(a, extra).ok, false, 'extra group is a tree fail');
}

{
  const a = dump(group({ iconTag: 'svg' }));
  const b = dump(group({ iconTag: 'div' }));
  assert.strictEqual(compareTickers(a, b).ok, false, 'company mark must stay an SVG');
}

console.log('ok - plan ticker shape: two numbers free, tree fixed');
