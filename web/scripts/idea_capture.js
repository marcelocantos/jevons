// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Pure helpers for durable idea intake (🎯T325.3).
// DOM-free so Node hermetic tests can require() it.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.IdeaCapture = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  /**
   * JSON body for POST /api/ideas.
   * @param {string} text
   * @param {string} [source] capture | idea | aside | chat | api
   * @param {string} [asideId]
   * @param {string} [domain]
   */
  function createIdeaRequestBody(text, source, asideId, domain) {
    const body = {
      text: String(text == null ? '' : text).trim(),
    };
    if (source) body.source = String(source);
    if (asideId) body.aside_id = String(asideId);
    if (domain) body.domain = String(domain);
    return body;
  }

  /**
   * Whether a composer result should dual-write the idea ledger.
   * @param {{ ideaCapture?: boolean, ideaText?: string }} result
   */
  function shouldCaptureIdea(result) {
    if (!result || !result.ideaCapture) return false;
    return String(result.ideaText || '').trim().length > 0;
  }

  /**
   * Build fetch args from handleComposer result.
   * @param {{ ideaText?: string, ideaSource?: string, threadId?: string }} result
   */
  function ideaCaptureFromComposer(result) {
    if (!shouldCaptureIdea(result)) return null;
    return createIdeaRequestBody(
      result.ideaText,
      result.ideaSource || 'capture',
      result.threadId || '',
      ''
    );
  }

  /** Next-ceremony hint for UI/docs (mirrors Go idea.NextCeremony). */
  function nextCeremony(disposition) {
    const d = String(disposition || 'inbox').toLowerCase();
    if (d === 'file') {
      return 'product-shaped → file bullseye (target: / jevons_target_file) + T193 if Build';
    }
    if (d === 'park') {
      return 'needs-owner/design → park-for-design; no unattended implementer';
    }
    if (d === 'hold') {
      return 'life-domain parked → hold; no implementer until owner unparks';
    }
    if (d === 'drop') {
      return 'one-off noise → drop with reason; prefer park when unsure';
    }
    return 'inbox → triage to file | park | hold | drop';
  }

  return {
    createIdeaRequestBody: createIdeaRequestBody,
    shouldCaptureIdea: shouldCaptureIdea,
    ideaCaptureFromComposer: ideaCaptureFromComposer,
    nextCeremony: nextCeremony,
  };
}));
