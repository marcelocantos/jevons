// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { type ClipboardEvent, type DragEvent, type FormEvent, useEffect, useRef, useState } from 'react';
import { useDrafts } from '../store/drafts';
import { normalizeDensity, type Density } from '../density';
import {
  composeSendText,
  filesFromTransfer,
  ingestPastedFiles,
  revokeObjectUrl,
  type ClipboardLike,
  type PendingImage,
} from '../composer/images';
import { applyComposerHomeEnd } from '../keys/composerCaret';
import { classifyEnterAction } from '../keys/composerEnter';
import type { DeliveryMode } from '../composer/deliveryMode';
import type { QueueItem } from '../composer/sendQueue';
import { cycleQueueFocus } from '../composer/queueFocus';

/** 🎯T657 slice 2b: the send queue the composer can focus with Alt+↑/↓. */
export type ComposerQueue = {
  items: QueueItem[];
  focusedId: string | null;
  onFocus: (id: string | null) => void;
  /** Send a queued item now with the given mode; the queue removes it. */
  onSend: (id: string, mode: DeliveryMode) => void;
};

export type RecalledRequest = { id: string; text: string };

type UserRequestProps = {
  name: string;
  density?: Density;
  /** 🎯T657: returns `{ queued: true }` when the text was held in the send queue instead of sent. */
  onSend: (text: string, opts?: { mode?: DeliveryMode }) => void | { queued?: boolean };
  onInterrupt?: () => void;
  queue?: ComposerQueue;
  disabled?: boolean;
  history?: RecalledRequest[];
  onRecall?: (request: RecalledRequest | null) => void;
  onRewind?: (request: RecalledRequest, text: string) => Promise<void>;
};

// A selected-agent change owns a new composer lifetime, including uploads and
// rewind responses still in flight for the previous agent.
export function UserRequest(props: UserRequestProps) {
  return <NamedUserRequest key={props.name} {...props} />;
}

function NamedUserRequest(props: UserRequestProps) {
  const density = normalizeDensity(props.density);
  const compact = density === 'compact';
  const liveDraft = useDrafts((s) => s.drafts[props.name] || '');
  const setDraft = useDrafts((s) => s.setDraft);
  const [pending, setPending] = useState<PendingImage[]>([]);
  const boxRef = useRef<HTMLTextAreaElement>(null);
  const [recalled, setRecalled] = useState<RecalledRequest | null>(null);
  const [recalledText, setRecalledText] = useState('');
  // Editing history never overwrites the ordinary persisted draft. Changing
  // agent cancels this local edit instead of turning it into an ordinary Send.
  const raw = recalled ? recalledText : liveDraft;
  const imagesBeforeRecall = useRef<PendingImage[]>([]);
  const pendingRef = useRef(pending);
  pendingRef.current = pending;
  const activeRef = useRef(true);
  const uploadsRef = useRef(0);
  const draftGeneration = useRef(0);
  const [rewinding, setRewinding] = useState(false);
  const [recallError, setRecallError] = useState('');
  const canSend = raw.trim().length > 0 || pending.length > 0;

  const leaveRecall = (restore: boolean) => {
    draftGeneration.current += 1;
    if (restore) {
      pending.forEach((img) => revokeObjectUrl(img.objectUrl));
      setPending(imagesBeforeRecall.current);
    } else {
      imagesBeforeRecall.current.forEach((img) => revokeObjectUrl(img.objectUrl));
    }
    imagesBeforeRecall.current = [];
    setRecalled(null);
    setRecalledText('');
    setRecallError('');
    props.onRecall?.(null);
  };

  const navigateHistory = (direction: -1 | 1) => {
    if (rewinding) return;
    if (uploadsRef.current) {
      setRecallError('Wait for the image upload to finish before recalling another request.');
      return;
    }
    const history = props.history || [];
    if (!history.length || (!recalled && direction === 1)) return;
    const current = recalled ? history.findIndex((r) => r.id === recalled.id) : history.length;
    if (current < 0) {
      setRecallError('This recalled request is no longer in the conversation. Cancel to restore your draft.');
      return;
    }
    if (!recalled) {
      imagesBeforeRecall.current = pending;
      setPending([]);
    }
    const next = Math.max(0, current + direction);
    if (next >= history.length) {
      leaveRecall(true);
      return;
    }
    const request = history[next];
    if (recalled && request.id !== recalled.id) {
      pending.forEach((img) => revokeObjectUrl(img.objectUrl));
      setPending([]);
    }
    draftGeneration.current += 1;
    setRecalled(request);
    setRecallError('');
    setRecalledText(request.text);
    props.onRecall?.(request);
    queueMicrotask(() => boxRef.current?.setSelectionRange(request.text.length, request.text.length));
  };

  useEffect(() => {
    activeRef.current = true;
    return () => {
      activeRef.current = false;
      pendingRef.current.forEach((img) => revokeObjectUrl(img.objectUrl));
      imagesBeforeRecall.current.forEach((img) => revokeObjectUrl(img.objectUrl));
    };
  }, []);

  const submit = async (e: FormEvent, append = false, opts?: { mode?: DeliveryMode }) => {
    e.preventDefault();
    if (rewinding) return;
    // 🎯T657: steer and interrupt are exactly the chords for a busy seat, so
    // the busy-disabled state must not swallow them.
    const mode = opts?.mode;
    if (props.disabled && mode !== 'interrupt' && mode !== 'steer') return;
    const payload = composeSendText(raw, pending);
    if (!payload) return;
    let queued = false;
    if (recalled && !append) {
      if (!props.history?.some((request) => request.id === recalled.id && request.text === recalled.text)) {
        setRecallError('This request changed or is no longer in the conversation. Cancel and select it again.');
        return;
      }
      if (!props.onRewind) {
        setRecallError('Rewind is unavailable for this conversation. You can cancel or send as a new message.');
        return;
      }
      setRewinding(true);
      setRecallError('');
      try {
        await props.onRewind(recalled, payload);
      } catch (error) {
        if (activeRef.current) setRecallError(error instanceof Error ? error.message : 'Rewind failed. Your edited request is preserved.');
        return;
      } finally {
        if (activeRef.current) setRewinding(false);
      }
      if (!activeRef.current) return;
      leaveRecall(true);
      queueMicrotask(() => boxRef.current?.focus());
      return;
    } else {
      const outcome = mode && mode !== 'submit' ? props.onSend(payload, { mode }) : props.onSend(payload);
      queued = !!(outcome && typeof outcome === 'object' && outcome.queued);
      if (recalled) {
        setDraft(props.name, payload);
        leaveRecall(false);
      }
    }
    pending.forEach((img) => revokeObjectUrl(img.objectUrl));
    setPending([]);
    // 🎯T545.3: keep the sent text until the transcript echoes a user row.
    // Failed send leaves composer + Send enabled for retry. A queued send
    // lives in the queue strip instead, so the composer clears at once.
    if (queued) setDraft(props.name, '');
    else if (payload !== raw) setDraft(props.name, payload);
    // 🎯T153: send returns focus so the next Tab stays on the box (T571).
    queueMicrotask(() => boxRef.current?.focus());
  };

  const attachFromTransfer = (data: ClipboardLike | null | undefined): boolean => {
    if (rewinding) return false;
    const files = filesFromTransfer(data);
    if (!files.length) return false;
    uploadsRef.current += 1;
    const generation = draftGeneration.current;
    void ingestPastedFiles(files).then((added) => {
      if (!activeRef.current || generation !== draftGeneration.current) {
        added.forEach((img) => revokeObjectUrl(img.objectUrl));
        return;
      }
      if (added.length) setPending((cur) => cur.concat(added));
    }).finally(() => { uploadsRef.current -= 1; });
    return true;
  };

  const onPaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    if (attachFromTransfer(e.clipboardData)) e.preventDefault();
  };

  const onDragOver = (e: DragEvent) => {
    if (filesFromTransfer(e.dataTransfer).length) e.preventDefault();
  };

  const onDrop = (e: DragEvent) => {
    if (attachFromTransfer(e.dataTransfer)) e.preventDefault();
  };

  const removeChip = (idx: number) => {
    setPending((cur) => {
      const img = cur[idx];
      revokeObjectUrl(img?.objectUrl);
      return cur.filter((_, i) => i !== idx);
    });
  };

  const boxId = compact ? 'agent-inspect-input' : 'input';
  const sendId = compact ? 'agent-inspect-send' : 'send';
  const imagesId = compact ? 'agent-inspect-composer-images' : 'composer-images';
  return (
    <div
      id={compact ? 'agent-inspect-composer' : 'input-bar'}
      className={compact ? 'cw-composer density-compact visible' : 'cw-composer density-comfortable'}
      onDragOver={onDragOver}
      onDrop={onDrop}
    >
      <div id={imagesId} className="composer-images" aria-label="Attached images">
        {pending.map((img, idx) => (
          <div key={img.id + '-' + idx} className="img-chip">
            <img src={img.objectUrl || img.thumbUrl || img.url} alt={'attachment ' + img.id} />
            <button type="button" title="Remove" onClick={() => removeChip(idx)}>
              ×
            </button>
          </div>
        ))}
      </div>
      {compact ? null : (
        <label htmlFor="input" className="sr-only">
          Compose a message for Jevons.
        </label>
      )}
      <textarea
        id={boxId}
        ref={boxRef}
        data-composer={compact ? 'sidebar' : 'main'}
        value={raw}
        onChange={(e) => recalled ? setRecalledText(e.target.value) : setDraft(props.name, e.target.value)}
        placeholder={compact ? 'Message this agent…' : 'Message...'}
        autoFocus={!compact}
        rows={1}
        disabled={rewinding}
        onPaste={onPaste}
        onKeyDown={(e) => {
          if (e.nativeEvent.isComposing) return;
          if (e.altKey && !e.metaKey && !e.ctrlKey && !e.shiftKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) {
            e.preventDefault();
            const dir = e.key === 'ArrowUp' ? -1 : 1;
            // 🎯T657: a non-empty send queue owns Alt+↑/↓; history recall
            // is only reachable once the queue is empty.
            const q = props.queue;
            if (q && q.items.length) {
              const r = cycleQueueFocus(q.focusedId, q.items, dir);
              if (r.handled) q.onFocus(r.focusedId);
              return;
            }
            navigateHistory(dir);
            return;
          }
          if (e.key === 'Escape' && props.queue?.focusedId) {
            e.preventDefault();
            props.queue.onFocus(null);
            return;
          }
          if (e.key === 'Escape' && recalled && !rewinding) {
            e.preventDefault();
            leaveRecall(true);
            return;
          }
          if (applyComposerHomeEnd(e.currentTarget, e)) return;
          const action = classifyEnterAction(e.key, e, {
            composerEmpty: !canSend,
            code: e.code,
          });
          if (action == null || action === 'newline') return;
          e.preventDefault();
          if (action === 'send') {
            void submit(e);
            return;
          }
          // 🎯T657 slice 2b: with a queue item focused, the steer and
          // interrupt chords act on that item, not on the draft.
          const focusedQueueId = props.queue?.focusedId && props.queue.items.some((it) => it.id === props.queue!.focusedId)
            ? props.queue.focusedId
            : null;
          if (action === 'steer') {
            if (focusedQueueId) {
              props.queue!.onSend(focusedQueueId, 'steer');
              props.queue!.onFocus(null);
              return;
            }
            // Cmd+Enter (🎯T657): fold the draft into the running turn; the
            // server sends plainly when the seat is idle. Nothing to steer
            // with on an empty composer, so that is a noop.
            if (canSend) void submit(e, !!recalled, { mode: 'steer' });
            return;
          }
          if (action === 'interrupt') {
            if (focusedQueueId) {
              props.queue!.onSend(focusedQueueId, 'interrupt');
              props.queue!.onFocus(null);
              return;
            }
            // Cmd+Shift+Enter (🎯T657): cancel the open turn, then send.
            if (canSend) void submit(e, !!recalled, { mode: 'interrupt' });
            else props.onInterrupt?.();
            return;
          }
          if (action === 'force_send' && canSend) {
            void submit(e, !!recalled, { mode: 'interrupt' });
          }
        }}
      />
      {recalled ? (
        <div className="composer-recall" role="group" aria-label="Editing an earlier request">
          <span>{props.onRewind ? 'Editing an earlier request. Enter rewinds and resends.' : 'Editing an earlier request. Rewind is not available yet.'}</span>
          <button type="button" disabled={rewinding} onClick={() => leaveRecall(true)}>Cancel edit</button>
          <button type="button" disabled={rewinding || !canSend} onClick={(e) => void submit(e, true)}>Send as new message</button>
        </div>
      ) : null}
      {recallError ? <div className="composer-recall-error" role="alert">{recallError}</div> : null}
      {compact ? null : (
        <>
          <span id="input-hint" className="sr-only">
            Type a prompt for the Jevons assistant.
          </span>
          <span id="wispr-context" className="sr-only" aria-live="polite" />
        </>
      )}
      <button
        id={sendId}
        type="button"
        disabled={rewinding || (props.disabled === true ? true : props.disabled === false ? false : !canSend)}
        onMouseDown={(e) => e.preventDefault()}
        onClick={(e) => submit(e as unknown as FormEvent)}
      >
        {rewinding ? 'Rewinding…' : recalled ? 'Rewind and resend' : 'Send'}
      </button>
    </div>
  );
}
