// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T540.1.12 J19 React load path. Isolate GET / may still be vanilla
// until 🎯T540.2; this is the :5173-style Vite proxy pointed at the
// isolate (never :13705). Loaded via ui/node_modules/.bin/vite --config.

import { createRequire } from 'node:module';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const DAILY_PORT = 13705;
const isolate = String(process.env.REACT_ISOLATE || process.env.J19_ISOLATE || '').trim();
if (!isolate) {
  throw new Error('j19_vite.config: REACT_ISOLATE or J19_ISOLATE (isolate host:port) is required');
}
if (isolate.indexOf(':' + DAILY_PORT) !== -1 || isolate === String(DAILY_PORT)) {
  throw new Error('j19_vite.config: refuses daily port ' + DAILY_PORT + ' (Universe A)');
}

const uiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..', 'ui');
const req = createRequire(path.join(uiRoot, 'package.json'));
const { defineConfig } = await import(pathToFileURL(req.resolve('vite')).href);
const reactMod = await import(pathToFileURL(req.resolve('@vitejs/plugin-react')).href);
const react = reactMod.default || reactMod;

export default defineConfig({
  root: uiRoot,
  base: './',
  plugins: [react()],
  server: {
    host: '127.0.0.1',
    strictPort: true,
    proxy: {
      '/api': 'http://' + isolate,
      '/health': 'http://' + isolate,
      '/ws': { target: 'ws://' + isolate, ws: true },
    },
  },
});
