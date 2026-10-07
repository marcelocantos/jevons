// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { type ChangeEvent, type ClipboardEvent, type DragEvent, type FormEvent, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { useDrafts } from '../store/drafts';
import { durableImage, usePendingImages } from '../store/pendingImages';
import { normalizeDensity, type Density } from '../density';
import {
  composeSendText,
  filesFromTransfer,
  objectUrlFor,
  revokeObjectUrl,
  uploadPastedImage,
  UploadError,
  type ClipboardLike,
  type PendingImage,
  type UploadedImage,
} from '../composer/images';
import { applyComposerHomeEnd } from '../keys/composerCaret';
import { classifyEnterAction, FORCE_SEND_MODE, isTouchPrimaryDevice } from '../keys/composerEnter';
import { isEffectivelyEmpty } from '../composer/wispr';
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
  /** When set, the box is held and this sentence replaces the invitation. */
  hold?: string;
  history?: RecalledRequest[];
  onRecall?: (request: RecalledRequest | null) => void;
  onRewind?: (request: RecalledRequest, text: string) => Promise<void>;
};

const NO_IMAGES: PendingImage[] = [];

/**
 * 🎯T1025: one pasted file's upload, drawn as a chip from the instant of the
 * paste. Never persisted: a reload cannot resume a fetch, and the store holds
 * only durable server ids.
 */
type UploadJob = {
  key: string;
  name: string;
  objectUrl?: string;
  status: 'uploading' | 'failed';
  error?: string;
  controller: AbortController;
};

const UPLOAD_WAIT_NOTICE = 'Wait for the image upload to finish before sending.';

// A selected-agent change owns a new composer lifetime, including uploads and
// rewind responses still in flight for the previous agent.
export function UserRequest(props: UserRequestProps) {
  return <NamedUserRequest key={props.name} {...props} />;
}

function NamedUserRequest(props: UserRequestProps) {
  const density = normalizeDensity(props.density);
  const compact = density === 'compact';
  const hint = props.hold || (compact ? 'Message this agent…' : 'Message...');
  const liveDraft = useDrafts((s) => s.drafts[props.name] || '');
  const setDraft = useDrafts((s) => s.setDraft);
  // 🎯T562.3: pending images persist per agent (agent switch and reload).
  // While a history request is recalled its images are local state and the
  // persisted set is left untouched, so a reload mid-edit loses nothing.
  const storedPending = usePendingImages((s) => s.images[props.name]) ?? NO_IMAGES;
  const setStoredPending = usePendingImages((s) => s.set);
  const [recalledPending, setRecalledPending] = useState<PendingImage[]>([]);
  const [recalled, setRecalled] = useState<RecalledRequest | null>(null);
  const pending: PendingImage[] = recalled ? recalledPending : storedPending;
  const setPending = (next: PendingImage[] | ((cur: PendingImage[]) => PendingImage[])) => {
    if (recalled) {
      setRecalledPending(next);
      return;
    }
    const cur = usePendingImages.getState().images[props.name] || NO_IMAGES;
    setStoredPending(props.name, (typeof next === 'function' ? next(cur) : next).map(durableImage));
  };
  const boxRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [recalledText, setRecalledText] = useState('');
  // Editing history never overwrites the ordinary persisted draft. Changing
  // agent cancels this local edit instead of turning it into an ordinary Send.
  const raw = recalled ? recalledText : liveDraft;
  const pendingRef = useRef(pending);
  pendingRef.current = pending;
  const activeRef = useRef(true);
  // 🎯T1025: in-flight and failed uploads for the composition open right now.
  const [jobs, setJobs] = useState<UploadJob[]>([]);
  const jobsRef = useRef(jobs);
  jobsRef.current = jobs;
  const jobSeq = useRef(0);
  const [uploadNotice, setUploadNotice] = useState('');
  const uploading = jobs.some((job) => job.status === 'uploading');
  // The composition an upload belongs to (🎯T1025): bumped when a message is
  // sent, a recall is entered or left, never by typing. A late upload checks
  // it before attaching, so it lands on the message it was pasted into or is
  // reported, never on a later one.
  const draftGeneration = useRef(0);
  const [rewinding, setRewinding] = useState(false);
  const [recallError, setRecallError] = useState('');
  const canSend = raw.trim().length > 0 || pending.length > 0;
  // 🎯T562.7: a seed-only composer holds no owner draft (T192).
  const hasRealDraft = !isEffectivelyEmpty(raw) || pending.length > 0 || jobs.length > 0;

  // The open composition is over (sent, recall entered or left): uploads
  // still running belong to it and are cancelled, not carried forward.
  const closeComposition = () => {
    draftGeneration.current += 1;
    jobsRef.current.forEach((job) => {
      job.controller.abort();
      revokeObjectUrl(job.objectUrl);
    });
    if (jobsRef.current.length) setJobs([]);
  };

  const leaveRecall = () => {
    closeComposition();
    if (recalled) recalledPending.forEach((img) => revokeObjectUrl(img.objectUrl));
    setRecalledPending([]);
    setRecalled(null);
    setRecalledText('');
    setRecallError('');
    props.onRecall?.(null);
  };

  const navigateHistory = (direction: -1 | 1) => {
    if (rewinding) return;
    if (uploading) {
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
    const next = Math.max(0, current + direction);
    if (next >= history.length) {
      leaveRecall();
      return;
    }
    const request = history[next];
    if (recalled && request.id !== recalled.id) {
      recalledPending.forEach((img) => revokeObjectUrl(img.objectUrl));
      setRecalledPending([]);
    }
    closeComposition();
    setRecalled(request);
    setRecallError('');
    setRecalledText(request.text);
    props.onRecall?.(request);
    queueMicrotask(() => boxRef.current?.setSelectionRange(request.text.length, request.text.length));
  };

  // 🎯T799: grow with the draft; the CSS max-height is the cap, and past it
  // the textarea scrolls inside. Runs on every draft change, so clearing after
  // a send shrinks it back to one line.
  useLayoutEffect(() => {
    const el = boxRef.current;
    if (!el) return;
    el.style.height = 'auto';
    // An empty box stays at its CSS min-height (a wrapping placeholder would inflate scrollHeight, 🎯T478).
    if (!raw) {
      el.classList.remove('composer-scroll');
      return;
    }
    const border = el.offsetHeight - el.clientHeight;
    el.style.height = `${el.scrollHeight + border}px`;
    el.classList.toggle('composer-scroll', el.scrollHeight > el.clientHeight + 1);
  }, [raw]);

  useEffect(() => {
    activeRef.current = true;
    return () => {
      activeRef.current = false;
      pendingRef.current.forEach((img) => revokeObjectUrl(img.objectUrl));
      jobsRef.current.forEach((job) => {
        job.controller.abort();
        revokeObjectUrl(job.objectUrl);
      });
    };
  }, []);

  const submit = async (e: FormEvent, append = false, opts?: { mode?: DeliveryMode }) => {
    e.preventDefault();
    if (rewinding) return;
    // 🎯T657: steer and interrupt are exactly the chords for a busy seat, so
    // the busy-disabled state must not swallow them.
    const mode = opts?.mode;
    if (props.disabled && mode !== 'interrupt' && mode !== 'steer') return;
    // 🎯T1025: the pasted image is for this message; it goes when the image is attached, not before.
    if (uploading) {
      setUploadNotice(UPLOAD_WAIT_NOTICE);
      return;
    }
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
      leaveRecall();
      queueMicrotask(() => boxRef.current?.focus());
      return;
    } else {
      const outcome = mode && mode !== 'submit' ? props.onSend(payload, { mode }) : props.onSend(payload);
      queued = !!(outcome && typeof outcome === 'object' && outcome.queued);
      if (recalled) {
        setDraft(props.name, payload);
        leaveRecall();
      }
    }
    pending.forEach((img) => revokeObjectUrl(img.objectUrl));
    setPending([]);
    closeComposition();
    setUploadNotice('');
    // 🎯T545.3: keep the sent text until the transcript echoes a user row.
    // Failed send leaves composer + Send enabled for retry. A queued send
    // lives in the queue strip instead, so the composer clears at once.
    if (queued) setDraft(props.name, '');
    else if (payload !== raw) setDraft(props.name, payload);
    // 🎯T153: send returns focus so the next Tab stays on the box (T571).
    queueMicrotask(() => boxRef.current?.focus());
  };

  // One upload, correlated to the composition that was open at paste time.
  const runUpload = async (job: UploadJob, file: File, generation: number, recalledAtPaste: boolean) => {
    let uploaded: UploadedImage | undefined;
    let failure: UploadError | undefined;
    try {
      uploaded = await uploadPastedImage(file, fetch, { signal: job.controller.signal });
    } catch (err) {
      failure = err instanceof UploadError ? err : new UploadError('network', err instanceof Error && err.message ? err.message : 'upload failed');
    }
    const dropJob = () => setJobs((cur) => cur.filter((j) => j.key !== job.key));
    if (!activeRef.current) {
      revokeObjectUrl(job.objectUrl);
      return;
    }
    if (generation !== draftGeneration.current) {
      // The message this was pasted into was sent or closed meanwhile. Say
      // so; never attach it to whatever the owner is composing now.
      revokeObjectUrl(job.objectUrl);
      dropJob();
      if (!failure) setUploadNotice(`${job.name} finished uploading after the message it was pasted into was closed, so it was not attached. Paste it again.`);
      else if (failure.kind !== 'cancelled') setUploadNotice(`Image upload failed: ${failure.message}.`);
      return;
    }
    if (failure) {
      if (failure.kind === 'cancelled') {
        revokeObjectUrl(job.objectUrl);
        dropJob();
        return;
      }
      setJobs((cur) => cur.map((j) => (j.key === job.key ? { ...j, status: 'failed', error: failure!.message } : j)));
      setUploadNotice(`Image upload failed: ${failure.message}. Remove it and paste again.`);
      return;
    }
    dropJob();
    const added: PendingImage = { ...uploaded!, objectUrl: job.objectUrl };
    setPending((cur) => cur.concat([added]));
    // A send held for this upload is no longer held.
    setUploadNotice((cur) => (cur === UPLOAD_WAIT_NOTICE ? '' : cur));
    // Persisted images render from the server thumb; only recall keeps blobs.
    if (!recalledAtPaste) revokeObjectUrl(job.objectUrl);
  };

  const attachFromTransfer = (data: ClipboardLike | null | undefined): boolean => {
    if (rewinding) return false;
    const files = filesFromTransfer(data);
    if (!files.length) return false;
    const generation = draftGeneration.current;
    setUploadNotice('');
    // 🎯T1025: the chip is on screen before the first byte goes out.
    const started: UploadJob[] = files.map((file) => ({
      key: `upload-${(jobSeq.current += 1)}`,
      name: file.name || 'Pasted image',
      objectUrl: objectUrlFor(file),
      status: 'uploading',
      controller: new AbortController(),
    }));
    setJobs((cur) => cur.concat(started));
    started.forEach((job, i) => void runUpload(job, files[i], generation, !!recalled));
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

  // 🎯T1020: mobile/touch devices have no paste/drag-drop clipboard access,
  // so a visible button + hidden file input is the only affordance that
  // reaches the camera/photo-picker sheet. Reuses attachFromTransfer
  // unchanged — a FileList already satisfies ClipboardLike's {files} shape.
  const onFileInputChange = (e: ChangeEvent<HTMLInputElement>) => {
    attachFromTransfer({ files: e.target.files ?? undefined });
    // Reset so picking the same file again still fires a change event.
    e.target.value = '';
  };

  const removeJob = (key: string) => {
    const job = jobsRef.current.find((j) => j.key === key);
    if (!job) return;
    job.controller.abort();
    revokeObjectUrl(job.objectUrl);
    setJobs((cur) => cur.filter((j) => j.key !== key));
    setUploadNotice('');
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
        {jobs.map((job) => (
          <div
            key={job.key}
            className={'img-chip img-chip-' + job.status}
            data-upload={job.status}
            role={job.status === 'uploading' ? 'status' : undefined}
            aria-label={job.status === 'uploading' ? 'Uploading ' + job.name + '…' : 'Upload of ' + job.name + ' failed'}
            title={job.status === 'uploading' ? 'Uploading ' + job.name + '…' : job.error}
          >
            {job.objectUrl ? <img src={job.objectUrl} alt="" /> : null}
            <span className="img-chip-state">{job.status === 'uploading' ? 'Uploading…' : 'Failed'}</span>
            <button type="button" title={job.status === 'uploading' ? 'Cancel upload' : 'Remove'} onClick={() => removeJob(job.key)}>
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
      {/* The hint is drawn over the empty box, not a placeholder attribute:
          macOS accessibility reports an empty field's placeholder as its
          value, so dictation (Wispr Flow) read "Message..." as text already
          typed and continued it mid-sentence — lowercase, leading space. */}
      <div className="composer-field">
        <textarea
          id={boxId}
          ref={boxRef}
          data-composer={compact ? 'sidebar' : 'main'}
          value={raw}
          onChange={(e) => recalled ? setRecalledText(e.target.value) : setDraft(props.name, e.target.value)}
          autoFocus={!compact && !props.hold}
          rows={1}
          title={props.hold || 'Enter send · ⌘Enter steer · ⌘⇧Enter interrupt · ⌥Enter force-send the draft (or the next queued item) · Alt+↑/↓ pick a queued item'}
          disabled={rewinding || props.disabled === true}
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
              leaveRecall();
              return;
            }
            if (applyComposerHomeEnd(e.currentTarget, e)) return;
            const action = classifyEnterAction(e.key, e, {
              composerEmpty: !hasRealDraft,
              queueLen: props.queue?.items.length ?? 0,
              code: e.code,
              touchPrimary: isTouchPrimaryDevice(),
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
            if (action === 'force_send') {
              void submit(e, !!recalled, { mode: FORCE_SEND_MODE });
              return;
            }
            if (action === 'send_queue_now') {
              // Alt+Enter with no real draft (T241): the focused item, else the queue head.
              const target = focusedQueueId ?? props.queue?.items[0]?.id;
              if (target) {
                props.queue!.onSend(target, FORCE_SEND_MODE);
                props.queue!.onFocus(null);
              }
            }
          }}
        />
        {raw === '' && hint ? (
          <div className="composer-hint" data-composer={compact ? 'sidebar' : 'main'} aria-hidden="true">
            {hint}
          </div>
        ) : null}
      </div>
      {recalled ? (
        <div className="composer-recall" role="group" aria-label="Editing an earlier request">
          <span>{props.onRewind ? 'Editing an earlier request. Enter rewinds and resends.' : 'Editing an earlier request. Rewind is not available yet.'}</span>
          <button type="button" disabled={rewinding} onClick={() => leaveRecall()}>Cancel edit</button>
          <button type="button" disabled={rewinding || !canSend} onClick={(e) => void submit(e, true)}>Send as new message</button>
        </div>
      ) : null}
      {recallError ? <div className="composer-recall-error" role="alert">{recallError}</div> : null}
      {uploadNotice ? <div className="composer-upload-notice" role="alert">{uploadNotice}</div> : null}
      {compact ? null : (
        <>
          <span id="input-hint" className="sr-only">
            Type a prompt for the Jevons assistant.
          </span>
        </>
      )}
      <input
        ref={fileInputRef}
        type="file"
        accept="image/*"
        multiple
        className="sr-only"
        tabIndex={-1}
        aria-hidden="true"
        onChange={onFileInputChange}
      />
      <button
        id={compact ? 'agent-inspect-attach' : 'attach-image'}
        type="button"
        className="composer-attach"
        title="Attach an image"
        aria-label="Attach an image"
        disabled={rewinding || props.disabled === true}
        onMouseDown={(e) => e.preventDefault()}
        onClick={() => fileInputRef.current?.click()}
      >
        {/* Plain glyph, matching the remove-chip × button's minimal style (🎯T1020: no existing icon-button convention in the composer to match). */}
        {'\u{1F4CE}'}
      </button>
      <button
        id={sendId}
        type="button"
        disabled={rewinding || uploading || (props.disabled === true ? true : props.disabled === false ? false : !canSend)}
        onMouseDown={(e) => e.preventDefault()}
        onClick={(e) => submit(e as unknown as FormEvent)}
      >
        {rewinding ? 'Rewinding…' : uploading ? 'Uploading…' : recalled ? 'Rewind and resend' : 'Send'}
      </button>
    </div>
  );
}
