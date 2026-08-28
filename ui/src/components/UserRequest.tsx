// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { type ClipboardEvent, type DragEvent, type FormEvent, useEffect, useState } from 'react';
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

export function UserRequest(props: {
  name: string;
  density?: Density;
  onSend: (text: string) => void;
  disabled?: boolean;
}) {
  const density = normalizeDensity(props.density);
  const compact = density === 'compact';
  const raw = useDrafts((s) => s.drafts[props.name] || '');
  const setDraft = useDrafts((s) => s.setDraft);
  const [pending, setPending] = useState<PendingImage[]>([]);
  const canSend = raw.trim().length > 0 || pending.length > 0;

  useEffect(() => {
    return () => {
      pending.forEach((img) => revokeObjectUrl(img.objectUrl));
    };
    // Unmount only: live removes revoke in removeChip / submit.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const payload = composeSendText(raw, pending);
    if (!payload) return;
    props.onSend(payload);
    pending.forEach((img) => revokeObjectUrl(img.objectUrl));
    setPending([]);
    // 🎯T545.3: keep the sent text until the transcript echoes a user row.
    // Failed send leaves composer + Send enabled for retry.
    if (payload !== raw) setDraft(props.name, payload);
  };

  const attachFromTransfer = (data: ClipboardLike | null | undefined): boolean => {
    const files = filesFromTransfer(data);
    if (!files.length) return false;
    void ingestPastedFiles(files).then((added) => {
      if (added.length) setPending((cur) => cur.concat(added));
    });
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
        data-composer={props.name === 'jevons' ? 'main' : 'sidebar'}
        value={raw}
        onChange={(e) => setDraft(props.name, e.target.value)}
        placeholder={compact ? 'Message this agent…' : 'Message...'}
        autoFocus={!compact}
        rows={1}
        onPaste={onPaste}
        onKeyDown={(e) => {
          if (applyComposerHomeEnd(e.currentTarget, e)) return;
          if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            submit(e);
          }
        }}
      />
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
        disabled={props.disabled === true ? true : props.disabled === false ? false : !canSend}
        onClick={(e) => submit(e as unknown as FormEvent)}
      >
        Send
      </button>
    </div>
  );
}
