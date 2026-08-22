// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useState, type FormEvent } from 'react';

export function UserRequest(props: { onSend: (text: string) => void; disabled?: boolean }) {
  const [text, setText] = useState('');
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const t = text.trim();
    if (!t) return;
    props.onSend(t);
    setText('');
  };
  return (
    <form className="user-request" onSubmit={submit}>
      <textarea
        value={text}
        onChange={(e) => setText(e.target.value)}
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
