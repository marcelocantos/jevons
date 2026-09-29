// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T505: uncaught errors in the owner's cockpit reach the durable event
// journal. The vanilla runtime posted window.onerror to /api/log; retiring it
// (0e949424) silently took the sensor with it, so from 2026-09-01 the journal
// carried no browser rows at all and "no WIP ReferenceError recurred" could
// not be observed. Each report carries the executing bundle's asset URL, so an
// incident can be traced to the committed ui/bundle.zip that served it.
//
// The payload keeps the vanilla shape — msg "window.onerror", component
// "window" — so staffops reads it as the same event:error:window symptom.

export const MAX_REPORTS_PER_PAGE = 20;

export interface ErrorReport {
  level: 'error';
  msg: 'window.onerror' | 'window.unhandledrejection';
  fields: Record<string, string | number>;
}

type Post = (report: ErrorReport) => void;

const postToJournal: Post = (report) => {
  void fetch('/api/log', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(report),
    keepalive: true,
  }).catch(() => {
    // Best-effort: a daemon that is down cannot journal its own absence.
  });
};

function describeReason(reason: unknown): { message: string; stack: string } {
  if (reason instanceof Error) {
    return { message: `${reason.name}: ${reason.message}`, stack: reason.stack ?? '' };
  }
  return { message: String(reason), stack: '' };
}

// installErrorReport wires window error and unhandledrejection listeners.
// Identical errors (a render loop rethrowing every frame) report once, and a
// page reports at most MAX_REPORTS_PER_PAGE — the vanilla sensor wrote the
// same ReferenceError dozens of times a minute. Returns an uninstaller.
export function installErrorReport(target: Window = window, post: Post = postToJournal, asset: string = import.meta.url): () => void {
  const seen = new Set<string>();
  let sent = 0;
  const report = (r: ErrorReport) => {
    const key = `${r.msg}|${r.fields.message}|${r.fields.filename ?? ''}|${r.fields.lineno ?? ''}`;
    if (seen.has(key) || sent >= MAX_REPORTS_PER_PAGE) return;
    seen.add(key);
    sent++;
    post(r);
  };

  const onError = (e: ErrorEvent) => {
    const { message, stack } = e.error !== undefined && e.error !== null ? describeReason(e.error) : { message: e.message, stack: '' };
    report({
      level: 'error',
      msg: 'window.onerror',
      fields: {
        component: 'window',
        message: message || e.message,
        filename: e.filename ?? '',
        lineno: e.lineno ?? 0,
        colno: e.colno ?? 0,
        stack,
        asset,
      },
    });
  };
  const onRejection = (e: PromiseRejectionEvent) => {
    const { message, stack } = describeReason(e.reason);
    report({
      level: 'error',
      msg: 'window.unhandledrejection',
      fields: { component: 'window', message, stack, asset },
    });
  };

  target.addEventListener('error', onError);
  target.addEventListener('unhandledrejection', onRejection);
  return () => {
    target.removeEventListener('error', onError);
    target.removeEventListener('unhandledrejection', onRejection);
  };
}
