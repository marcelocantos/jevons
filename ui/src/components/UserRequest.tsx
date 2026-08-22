// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { type FormEvent } from 'react';
import { useDrafts } from '../store/drafts';

export function UserRequest(props: {
  name: string;
  onSend: (text: string) => void;
  disabled?: boolean;
}) {
  const text = useDrafts((s) => s.drafts[props.name] || '');
  const setDraft = useDrafts((s) => s.setDraft);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const t = text.trim();
    if (!t) return;
    props.onSend(t);
    setDraft(props.name, '');
  };
  return (
    <form className="user-request" onSubmit={submit}>
      <textarea
        data-composer={props.name === 'jevons' ? 'main' : 'sidebar'}
        value={text}
        onChange={(e) => setDraft(props.name, e.target.value)}
        placeholder="Message"
        rows={3}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            submit(e);
          }
        }}
      />
      <button type="submit" disabled={props.disabled || !text.trim()}>
        Send
      </button>
    </form>
  );
}
