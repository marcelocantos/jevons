// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T1044: owner review UI. Lists durable review items from GET /api/reviews
// (produced by 🎯T1042 detection) with the context needed to act: target id
// and title, the exact question, evidence (commit/gate/diff/report/
// screenshots) with their verification status, and an "answer in chat"
// route that hands the selected item into the ordinary owner-chat composer
// — never a separate device-pairing or credential-auth channel (🎯T1044
// acceptance 2).

import { useQuery } from '@tanstack/react-query';
import { useDrafts } from '../store/drafts';
import { focusMainComposer } from '../keys/composerFocus';

export type ReviewArtifact = {
  status: string; // missing | reported_only | inaccessible | verified
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

export function ReviewCard(props: { item: ReviewItem; onAnswerInChat: (item: ReviewItem) => void }) {
  const item = props.item;
  const ev = item.evidence;
  return (
    <div className="review-card" data-review-id={item.id} data-target={item.target}>
      <div className="review-card-head">
        <span className="review-target">🎯{item.target || '?'}</span>
        <span className="review-repo">{item.repository}</span>
        <span className={'review-state review-state-' + item.state}>{item.state}</span>
        <span className={'review-readiness review-readiness-' + item.readiness}>{item.readiness}</span>
      </div>
      {item.target_title ? <div className="review-target-title">{item.target_title}</div> : null}
      <div className="review-question">{item.question}</div>
      {item.options && item.options.length ? (
        <div className="review-options">Options: {item.options.join(' | ')}</div>
      ) : null}
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
        ) : (
          <div className="review-artifact review-artifact-missing">
            <span className="review-artifact-label">Screenshots:</span> <span className="review-artifact-value">missing</span>
          </div>
        )}
      </div>
      <div className="review-card-actions">
        <button type="button" className="review-answer-btn" onClick={() => props.onAnswerInChat(item)}>
          Answer in chat
        </button>
      </div>
    </div>
  );
}

/** Reviews are owner-review items; the dedicated, discoverable listing for 🎯T1044. */
export function ReviewPanel(props: { active: boolean; onAnswerInChat?: (item: ReviewItem) => void }) {
  const setDraft = useDrafts((s) => s.setDraft);
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
    // The owner's approval/rejection/free-text answer is submitted through
    // the ordinary owner-chat mechanism — never a separate device-pairing or
    // credential-auth channel (🎯T1044 acceptance 2). The root overseer
    // conversation (name="jevons") is always mounted; route there.
    setDraft('jevons', reviewAnswerDraft(item));
    focusMainComposer();
    props.onAnswerInChat?.(item);
  };

  const items = q.data || [];
  return (
    <div id="review-pane-body" className="review-pane-body">
      {q.isLoading ? <div className="review-empty">loading…</div> : null}
      {q.isError ? <div className="review-empty review-error">review index unavailable</div> : null}
      {!q.isLoading && !q.isError && items.length === 0 ? (
        <div className="review-empty" id="review-empty">
          Nothing pending owner review.
        </div>
      ) : null}
      {items.map((item) => (
        <ReviewCard key={item.id} item={item} onAnswerInChat={answerInChat} />
      ))}
    </div>
  );
}
