// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useMemo } from 'react';

/**
 * The dangerouslySetInnerHTML value for html, stable while html is.
 *
 * React 19 rewrites a node's innerHTML whenever the {__html} object changes
 * identity, not only when the string does. An inline `{{ __html: html }}`
 * therefore replaced every transcript body on every re-render: on 2026-10-01
 * the cockpit tore down and rebuilt ~780 nodes every 10 s while nothing
 * changed, clobbering the owner's text selection, repainting mermaid
 * diagrams and flickering the bubbles.
 */
export function useInnerHTML(html: string): { __html: string } {
  return useMemo(() => ({ __html: html }), [html]);
}
