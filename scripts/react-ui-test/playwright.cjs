// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Loads Playwright for the packaged-React journeys. node_modules is untracked,
// so a clean worktree (bin/gate -clean) has none: fall back to the main
// checkout's install, found through git's common dir (🎯T805). Read-only use;
// nothing is linked into or installed in the worktree.
const path = require('node:path');
const { execFileSync } = require('node:child_process');

module.exports = function loadPlaywright() {
  const rel = path.join('scripts', 'browser-loop-test', 'node_modules', 'playwright');
  try {
    return require(path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright'));
  } catch (err) {
    if (err.code !== 'MODULE_NOT_FOUND') throw err;
  }
  const common = execFileSync('git', ['rev-parse', '--path-format=absolute', '--git-common-dir'], { cwd: __dirname }).toString().trim();
  return require(path.join(path.dirname(common), rel));
};
