// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Pin-to-end screen Y for the first and last transcript rows.
 *
 * #messages is below the status bar (messagesTop) with padding padTop/padBottom
 * and clientHeight ch. Canvas height = contentHeight + paddingEnd.
 * scrollTop = padTop + contentHeight + paddingEnd + padBottom - ch.
 *
 * first = messagesTop + padTop - scrollTop
 *       = messagesTop + ch - padBottom - contentHeight - paddingEnd
 * last  = first + contentHeight - lastHeight
 *       = messagesTop + ch - padBottom - paddingEnd - lastHeight
 *
 * paddingEnd (a blank band after the last row) shifts first AND last by the
 * same amount. Extra content between the first and last rows shifts only
 * first. Those two knobs are not interchangeable — extra-14 at the bottom
 * was why first-bubble ink and the last ⋯ 13 steps could not both sit on
 * the golden.
 */
export type PinGeometry = {
  messagesTop: number;
  padTop: number;
  padBottom: number;
  clientHeight: number;
  contentHeight: number;
  paddingEnd: number;
  lastHeight: number;
};

export function pinnedFirstY(g: PinGeometry): number {
  return g.messagesTop + g.clientHeight - g.padBottom - g.contentHeight - g.paddingEnd;
}

export function pinnedLastY(g: PinGeometry): number {
  return g.messagesTop + g.clientHeight - g.padBottom - g.paddingEnd - g.lastHeight;
}
