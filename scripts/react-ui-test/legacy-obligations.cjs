// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Accounting only: this never certifies legacy behavior as implemented.
const assert = require('node:assert/strict');
const { execFileSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const root = path.resolve(__dirname, '../..');
const reference = 'bc303b0dc044695e0757864022bbe6197f9b7f9f';
const git = (...args) => execFileSync('git', args, { cwd: root, encoding: 'utf8' });
const manifest = JSON.parse(fs.readFileSync(path.join(root, 'docs/audits/react-retirement-2026-09-05/legacy-suites.json')));
assert.equal(manifest.reference_commit, reference);
const make = git('show', `${reference}:Makefile`);
const expected = [];
let entrypoint = '';
for (const line of make.split('\n')) {
  if (line && !line.startsWith('\t') && !line.startsWith('#')) entrypoint = /^(test-web|test-ui):/.exec(line)?.[1] || '';
  const command = /^\tnode (\S+)\s*$/.exec(line);
  if (entrypoint && command) expected.push({ entrypoint, path: command[1] });
}
assert(expected.length > 0, 'frozen standing entrypoints not found');
assert.deepEqual(manifest.suites.map(({ entrypoint, path }) => ({ entrypoint, path })), expected, 'legacy obligations were omitted or added without reference evidence');
for (const suite of manifest.suites) {
  assert.equal(suite.blob, git('rev-parse', `${reference}:${suite.path}`).trim());
  assert.equal(suite.disposition, 'open', 'changing disposition requires exact successor coverage or reviewed scope evidence');
  assert.equal(suite.obligation_target, 'T540.3');
  const lines = git('show', suite.blob).split('\n');
  for (const label of suite.test_labels) assert.equal(lines[label.line - 1].trim(), label.text.trim(), `${suite.path}:${label.line}`);
  for (const line of suite.assertion_lines) assert(Number.isInteger(line) && line > 0 && line <= lines.length);
}
console.log(`PASS: all ${expected.length} frozen legacy suites retain explicit open obligations; this is not a behavior-coverage result.`);
