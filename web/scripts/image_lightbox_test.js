// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic oracle for 🎯T258 chat image lightbox + multi-id carousel model.
// Run: node web/scripts/image_lightbox_test.js

'use strict';

const assert = require('assert');
const ImageLightbox = require('./image_lightbox.js');

let passed = 0;
function test(name, fn) {
  try {
    fn();
    passed++;
    console.log('ok  -', name);
  } catch (e) {
    console.error('FAIL -', name);
    console.error(e && e.stack ? e.stack : e);
    process.exitCode = 1;
  }
}

// ── URL helpers ────────────────────────────────────────────────

test('imageFullSrc is /api/images/{id} (not thumb)', () => {
  assert.strictEqual(
    ImageLightbox.imageFullSrc('DeadBeefCafeBabe'),
    '/api/images/deadbeefcafebabe',
  );
  assert.ok(!ImageLightbox.imageFullSrc('abc').includes('/thumb'));
});

test('imageThumbSrc is /api/images/{id}/thumb', () => {
  assert.strictEqual(
    ImageLightbox.imageThumbSrc('ABCDEF01'),
    '/api/images/abcdef01/thumb',
  );
});

test('empty id yields empty URL', () => {
  assert.strictEqual(ImageLightbox.imageFullSrc(''), '');
  assert.strictEqual(ImageLightbox.imageThumbSrc(null), '');
});

test('parseIdFromSrc handles full and thumb paths', () => {
  assert.strictEqual(
    ImageLightbox.parseIdFromSrc('/api/images/deadbeefcafebabe'),
    'deadbeefcafebabe',
  );
  assert.strictEqual(
    ImageLightbox.parseIdFromSrc('/api/images/deadbeefcafebabe/thumb'),
    'deadbeefcafebabe',
  );
  assert.strictEqual(
    ImageLightbox.parseIdFromSrc('https://host/api/images/aa11bb22/thumb?x=1'),
    'aa11bb22',
  );
  assert.strictEqual(ImageLightbox.parseIdFromSrc('/other'), '');
});

// ── Marker / sibling extraction ────────────────────────────────

test('extractImageIdsFromText ordered unique', () => {
  const ids = ImageLightbox.extractImageIdsFromText(
    'hi [image: aaa111] mid [image: BBB222] [image: aaa111] end',
  );
  assert.deepStrictEqual(ids, ['aaa111', 'bbb222']);
});

test('extractImageIdsFromText empty when no markers', () => {
  assert.deepStrictEqual(ImageLightbox.extractImageIdsFromText('no images'), []);
  assert.deepStrictEqual(ImageLightbox.extractImageIdsFromText(null), []);
});

test('siblingIdsFromImgs reads data-image-id', () => {
  const fake = [
    { getAttribute: (k) => (k === 'data-image-id' ? 'aa11' : null) },
    { getAttribute: (k) => (k === 'data-image-id' ? 'bb22' : null) },
    { getAttribute: (k) => (k === 'data-image-id' ? 'aa11' : null) },
  ];
  assert.deepStrictEqual(ImageLightbox.siblingIdsFromImgs(fake), ['aa11', 'bb22']);
});

// ── Session open / close ───────────────────────────────────────

test('open single id', () => {
  const s = ImageLightbox.open({ ids: ['aa11'], index: 0 });
  assert.strictEqual(s.open, true);
  assert.strictEqual(ImageLightbox.currentId(s), 'aa11');
  assert.strictEqual(ImageLightbox.hasMultiple(s), false);
  assert.strictEqual(ImageLightbox.isOpen(s), true);
});

test('open multi with start id', () => {
  const s = ImageLightbox.open({ ids: ['aa', 'bb', 'cc'], id: 'BB' });
  assert.strictEqual(s.index, 1);
  assert.strictEqual(ImageLightbox.currentId(s), 'bb');
  assert.strictEqual(ImageLightbox.hasMultiple(s), true);
});

test('open empty ids is closed', () => {
  const s = ImageLightbox.open({ ids: [] });
  assert.strictEqual(s.open, false);
  assert.strictEqual(ImageLightbox.isOpen(s), false);
});

test('close dismisses', () => {
  const s = ImageLightbox.open({ ids: ['aa', 'bb'], index: 1 });
  const c = ImageLightbox.close(s);
  assert.strictEqual(c.open, false);
  assert.strictEqual(ImageLightbox.isOpen(c), false);
  assert.strictEqual(c.index, 1); // remember position
});

// ── Carousel model ─────────────────────────────────────────────

test('next/prev wrap among siblings', () => {
  let s = ImageLightbox.open({ ids: ['a', 'b', 'c'], index: 0 });
  s = ImageLightbox.next(s);
  assert.strictEqual(ImageLightbox.currentId(s), 'b');
  s = ImageLightbox.next(s);
  assert.strictEqual(ImageLightbox.currentId(s), 'c');
  s = ImageLightbox.next(s);
  assert.strictEqual(ImageLightbox.currentId(s), 'a');
  s = ImageLightbox.prev(s);
  assert.strictEqual(ImageLightbox.currentId(s), 'c');
});

test('next on single is no-op index', () => {
  let s = ImageLightbox.open({ ids: ['only'], index: 0 });
  s = ImageLightbox.next(s);
  assert.strictEqual(s.index, 0);
  assert.strictEqual(ImageLightbox.currentId(s), 'only');
});

test('goTo clamps', () => {
  let s = ImageLightbox.open({ ids: ['a', 'b'], index: 0 });
  s = ImageLightbox.goTo(s, 99);
  assert.strictEqual(s.index, 1);
  s = ImageLightbox.goTo(s, -3);
  assert.strictEqual(s.index, 0);
});

// ── Keyboard ───────────────────────────────────────────────────

test('handleKey Esc closes', () => {
  const s = ImageLightbox.open({ ids: ['a', 'b'], index: 0 });
  const r = ImageLightbox.handleKey(s, 'Escape');
  assert.strictEqual(r.action, 'close');
  assert.strictEqual(r.session.open, false);
});

test('handleKey arrows step when multi', () => {
  let s = ImageLightbox.open({ ids: ['a', 'b', 'c'], index: 1 });
  let r = ImageLightbox.handleKey(s, 'ArrowRight');
  assert.strictEqual(r.action, 'next');
  assert.strictEqual(ImageLightbox.currentId(r.session), 'c');
  r = ImageLightbox.handleKey(r.session, 'ArrowLeft');
  assert.strictEqual(r.action, 'prev');
  assert.strictEqual(ImageLightbox.currentId(r.session), 'b');
});

test('handleKey arrows noop when single', () => {
  const s = ImageLightbox.open({ ids: ['only'], index: 0 });
  const r = ImageLightbox.handleKey(s, 'ArrowRight');
  assert.strictEqual(r.action, null);
  assert.strictEqual(r.session.index, 0);
  assert.strictEqual(r.session.open, true);
});

test('handleKey when closed is null', () => {
  const s = ImageLightbox.close(ImageLightbox.open({ ids: ['a'] }));
  const r = ImageLightbox.handleKey(s, 'Escape');
  assert.strictEqual(r.action, null);
});

// ── resolveFromClick ───────────────────────────────────────────

test('resolveFromClick collects message siblings', () => {
  const imgs = [
    { getAttribute: (k) => (k === 'data-image-id' ? 'aa11' : null) },
    { getAttribute: (k) => (k === 'data-image-id' ? 'bb22' : null) },
  ];
  const clicked = {
    getAttribute: (k) => (k === 'data-image-id' ? 'bb22' : null),
    closest: () => ({
      querySelectorAll: () => imgs,
    }),
  };
  const r = ImageLightbox.resolveFromClick(clicked);
  assert.deepStrictEqual(r.ids, ['aa11', 'bb22']);
  assert.strictEqual(r.id, 'bb22');
});

if (!process.exitCode) {
  console.log('PASS image_lightbox_test (' + passed + ' tests, T258)');
}
