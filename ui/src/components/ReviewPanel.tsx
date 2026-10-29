// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T1044 + 🎯T404.1: open owner questions and review items from GET
// /api/reviews. T404.1 surfaces the question contract at a glance (asker,
// what is waiting, default_on_silence, options) and answers in place via
// owner-authenticated POST /api/reviews/{id}/answer.

import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useDrafts } from '../store/drafts';
import { focusMainComposer } from '../keys/composerFocus';

export type ReviewArtifact = {
  status: string;
  reference?: string;
  verdict?: string;
};

export type ReviewEvidence = {
  commit: ReviewArtifact;
  gate: ReviewArtifact;
  diff: ReviewArtifact;
  report: ReviewArtifact;
  screenshots: ReviewArtifact[];
};

export type ReviewItem = {
  schema_version: number;
  target_title?: string;
  target_status?: string;
  target_lookup: string;
  evidence: ReviewEvidence;
  id: string;
  url: string;
  repository: string;
  target: string;
  ask_id: string;
  version: string;
  question: string;
  asker: string;
  answer_route: string;
  default_on_silence: string;
  options?: string[];
  state: string;
  readiness: string;
  prerequisite?: string;
  action?: string;
  evidence_status?: string;
  resolution?: string;
};

const STALLS_PREFIX = 'STALLS';

/** True when the asker declared no default-on-silence (🎯T404). */
export function reviewIsStalling(defaultOnSilence: string): boolean {
  return (defaultOnSilence || '').trim().startsWith(STALLS_PREFIX);
}

export async function postReviewAnswer(reviewId: string, answer: string): Promise<{ status: string; routed?: string }> {
  const r = await fetch('/api/reviews/' + encodeURIComponent(reviewId) + '/answer', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ answer }),
  });
  const data = await r.json().catch(() => ({}));
  if (!r.ok) {
    throw new Error(typeof data.error === 'string' ? data.error : 'answer HTTP ' + r.status);
  }
  return data;
}

/** Pure: compose the "answer in chat" draft text for a review item. */
export function reviewAnswerDraft(item: ReviewItem): string {
  const lines: string[] = [];
  const target = item.target ? '🎯' + item.target : '';
  const head = [target, item.repository].filter(Boolean).join(' ');
  lines.push(('Re ' + head).trim() + (item.target_title ? ': ' + item.target_title : '') + ':');
  lines.push(item.question || '(no question text)');
  if (item.options && item.options.length) {
    lines.push('Options: ' + item.options.join(' | '));
  }
  lines.push('');
  lines.push('My answer: ');
  return lines.join('\n');
}

function artifactLabel(a: ReviewArtifact | undefined): string {
  if (!a) return 'missing';
  if (a.status === 'verified') return 'verified' + (a.reference ? ' · ' + a.reference : '');
  if (a.status === 'reported_only') return 'reported (unverified)' + (a.reference ? ' · ' + a.reference : '');
  if (a.status === 'inaccessible') return 'inaccessible' + (a.reference ? ' · ' + a.reference : '');
  return 'missing';
}

function ArtifactRow(props: { label: string; artifact: ReviewArtifact | undefined }) {
  const a = props.artifact;
  const cls = 'review-artifact review-artifact-' + (a ? a.status : 'missing');
  return (
    <div className={cls}>
      <span className="review-artifact-label">{props.label}:</span>{' '}
      <span className="review-artifact-value">{artifactLabel(a)}</span>
      {a?.verdict ? <span className="review-artifact-verdict"> ({a.verdict})</span> : null}
    </div>
  );
}

function hasReviewEvidence(item: ReviewItem): boolean {
  const ev = item.evidence;
  if (!ev) return false;
  return (
    ev.commit?.status !== 'missing' ||
    ev.gate?.status !== 'missing' ||
    ev.diff?.status !== 'missing' ||
    ev.report?.status !== 'missing' ||
    (ev.screenshots?.length ?? 0) > 0 ||
    item.evidence_status === 'reported-unverified'
  );
}

export function ReviewCard(props: {
  item: ReviewItem;
  onAnswerInChat: (item: ReviewItem) => void;
  onAnswered: (item: ReviewItem, answer: string) => void;
}) {
  const item = props.item;
  const ev = item.evidence;
  const stalling = reviewIsStalling(item.default_on_silence);
  const closed = (item.options?.length ?? 0) > 0;
  const [prose, setProse] = useState('');
  const [err, setErr] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const submit = async (answer: string) => {
    setErr('');
    setSubmitting(true);
    try {
      await postReviewAnswer(item.id, answer);
      props.onAnswered(item, answer);
      setProse('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="review-card" data-review-id={item.id} data-target={item.target} data-stalling={stalling ? 'true' : 'false'}>
      <div className="review-card-head">
        <span className="review-target">🎯{item.target || '?'}</span>
        <span className="review-repo">{item.repository}</span>
        {stalling ? <span className="review-stalls-mark">STALLS</span> : null}
        <span className={'review-state review-state-' + item.state}>{item.state}</span>
      </div>
      {item.target_title ? <div className="review-target-title">{item.target_title}</div> : null}
      <div className="review-question">{item.question}</div>
      <dl className="review-meta">
        <div>
          <dt>Asker</dt>
          <dd>{item.asker || '—'}</dd>
        </div>
        <div>
          <dt>Waiting on</dt>
          <dd>{item.answer_route || '—'}</dd>
        </div>
        <div>
          <dt>If unanswered</dt>
          <dd className={stalling ? 'review-default-stalls' : 'review-default-ok'}>{item.default_on_silence || '—'}</dd>
        </div>
      </dl>
      {closed ? (
        <div className="review-option-picks" role="group" aria-label="Choose an option">
          {item.options!.map((opt) => (
            <button
              key={opt}
              type="button"
              className="review-option-btn"
              disabled={submitting}
              onClick={() => submit(opt)}
            >
              {opt}
            </button>
          ))}
        </div>
      ) : null}
      <div className="review-answer-in-place">
        <label className="review-prose-label" htmlFor={'review-answer-' + item.id}>
          {closed ? 'Or answer in prose' : 'Your answer'}
        </label>
        <textarea
          id={'review-answer-' + item.id}
          className="review-prose-input"
          rows={closed ? 2 : 3}
          value={prose}
          disabled={submitting}
          onChange={(e) => setProse(e.target.value)}
          placeholder={closed ? 'Free-form override…' : 'Type your decision…'}
        />
        <button
          type="button"
          className="review-submit-answer-btn"
          disabled={submitting || !prose.trim()}
          onClick={() => submit(prose.trim())}
        >
          Submit answer
        </button>
      </div>
      {err ? <div className="review-answer-error">{err}</div> : null}
      {hasReviewEvidence(item) ? (
        <div className="review-evidence">
          <ArtifactRow label="Commit" artifact={ev?.commit} />
          <ArtifactRow label="Gate" artifact={ev?.gate} />
          <ArtifactRow label="Diff" artifact={ev?.diff} />
          <ArtifactRow label="Report" artifact={ev?.report} />
          {ev?.screenshots?.length ? (
            <div className="review-artifact review-artifact-screenshots">
              <span className="review-artifact-label">Screenshots:</span>{' '}
              {ev.screenshots.map((s, i) => (
                <span key={i} className={'review-screenshot review-artifact-' + s.status}>
                  {s.reference}
                  {i < ev.screenshots.length - 1 ? ', ' : ''}
                </span>
              ))}
            </div>
          ) : null}
        </div>
      ) : null}
      <div className="review-card-actions">
        <button type="button" className="review-answer-btn review-answer-chat-fallback" onClick={() => props.onAnswerInChat(item)}>
          Answer in chat instead
        </button>
      </div>
    </div>
  );
}

/** Open owner questions at a glance (🎯T404.1) plus 🎯T1044 review evidence when present. */
export function ReviewPanel(props: { active: boolean; onAnswerInChat?: (item: ReviewItem) => void }) {
  const setDraft = useDrafts((s) => s.setDraft);
  const qc = useQueryClient();
  const q = useQuery<ReviewItem[]>({
    queryKey: ['reviews'],
    queryFn: async () => {
      const r = await fetch('/api/reviews');
      if (!r.ok) throw new Error('reviews HTTP ' + r.status);
      const data = await r.json();
      return Array.isArray(data) ? data : [];
    },
    enabled: props.active,
    staleTime: 10_000,
    refetchInterval: props.active ? 15_000 : false,
  });

  const answerInChat = (item: ReviewItem) => {
    setDraft('jevons', reviewAnswerDraft(item));
    focusMainComposer();
    props.onAnswerInChat?.(item);
  };

  const items = q.data || [];
  return (
    <div id="review-pane-body" className="review-pane-body open-questions-pane">
      {q.isLoading ? <div className="review-empty">loading…</div> : null}
      {q.isError ? <div className="review-empty review-error">open questions unavailable</div> : null}
      {!q.isLoading && !q.isError && items.length === 0 ? (
        <div className="review-empty" id="review-empty">
          No open owner questions.
        </div>
      ) : null}
      {items.map((item) => (
        <ReviewCard
          key={item.id}
          item={item}
          onAnswerInChat={answerInChat}
          onAnswered={() => qc.invalidateQueries({ queryKey: ['reviews'] })}
        />
      ))}
    </div>
  );
}
