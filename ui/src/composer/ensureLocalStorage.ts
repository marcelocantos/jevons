// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** jsdom + Node can expose a localStorage object without setItem. */

type MapStorage = Storage & { _map: Record<string, string> };

function createMemoryStorage(): MapStorage {
  const map: Record<string, string> = {};
  return {
    getItem: (k: string) => (Object.prototype.hasOwnProperty.call(map, k) ? map[k] : null),
    setItem: (k: string, v: string) => {
      map[k] = String(v);
    },
    removeItem: (k: string) => {
      delete map[k];
    },
    clear: () => {
      for (const key of Object.keys(map)) delete map[key];
    },
    key: (i: number) => Object.keys(map)[i] ?? null,
    get length() {
      return Object.keys(map).length;
    },
    _map: map,
  };
}

function needsPolyfill(storage: unknown): boolean {
  return !storage || typeof (storage as Storage).setItem !== 'function';
}

const installed = createMemoryStorage();

if (typeof globalThis !== 'undefined' && needsPolyfill(globalThis.localStorage)) {
  Object.defineProperty(globalThis, 'localStorage', { value: installed, configurable: true });
}
if (typeof window !== 'undefined' && needsPolyfill(window.localStorage)) {
  Object.defineProperty(window, 'localStorage', { value: installed, configurable: true });
}

export { installed as memoryLocalStorage };
