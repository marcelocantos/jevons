// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useQuery } from '@tanstack/react-query';
import { useDrafts } from '../store/drafts';

export type Review = {
  id: string; url: string; repository: string; target: string; target_title?: string;
  ask_id: string; version: string; question: string; state: string;
  readiness: string; prerequisite?: string; action?: string;
  evidence_status?: string; resolution?: string;
  target_status?: string; target_lookup?: 'verified' | 'missing' | 'inaccessible'; schema_version?: number;
  evidence?: Record<string, unknown>;
};

async function readReviews<T>(path: string): Promise<T> {
  const response = await fetch(path);
  if (!response.ok) throw new Error(response.status === 404 ? 'Review not found' : 'Reviews unavailable');
  return response.json() as Promise<T>;
}

export function useReviews() {
  return useQuery({ queryKey: ['reviews'], queryFn: () => readReviews<Review[]>('/api/reviews'), refetchInterval: 15_000 });
}

export function reviewLabel(review: Review) {
  return `${review.repository || 'Unknown repository'} · ${review.target || 'Unknown target'}${review.target_lookup === 'verified' && review.target_title ? ` — ${review.target_title}` : ''}`;
}

export function reviewReadiness(review: Review) {
  if (review.state !== 'open') return `Closed (${review.state})`;
  if (review.readiness === 'actionable') return 'Ready for owner review';
  if (review.readiness === 'prerequisite_blocked' && review.prerequisite) return `Blocked on prerequisite: ${review.prerequisite}`;
  return 'Review event missing or unverified — readiness unknown';
}

// Only a producer-verified manifest may turn reported artifacts into links.
// Untrusted paths and URL schemes never become navigable from this UI.
export function verifiedEvidenceLinks(review: Review) {
  const entries = review.evidence || {};
  const links: { kind: string; url: string }[] = [];
  for (const [kind, item] of Object.entries(entries)) {
    const candidates = Array.isArray(item) ? item : [item];
    for (const candidate of candidates) {
      if (!candidate || typeof candidate !== 'object') continue;
      const value = candidate as Record<string, unknown>;
      if (value.status !== 'verified' || typeof value.url !== 'string') continue;
      try {
        const url = new URL(value.url, window.location.origin);
        if (url.origin === window.location.origin && !value.url.startsWith('//')) {
          links.push({ kind, url: value.url });
        }
      } catch { /* malformed source reference is not a link */ }
    }
  }
  return links;
}

export function evidenceSummary(review: Review) {
  if (!review.evidence) return 'Evidence missing or unverified. Reported artifacts are not verified download links.';
  return Object.entries(review.evidence).map(([kind, item]) => {
    const candidates = Array.isArray(item) ? item : [item];
    return candidates.map((candidate, index) => {
      const value = candidate && typeof candidate === 'object' ? candidate as Record<string, unknown> : {};
      const status = typeof value.status === 'string' ? value.status : 'unverified';
      const reference = status === 'verified' && typeof value.reference === 'string' ? ` · ${value.reference}` : '';
      const verdict = status === 'verified' && typeof value.verdict === 'string' ? ` · ${value.verdict}` : '';
      return `${kind}${candidates.length > 1 ? ` ${index + 1}` : ''}: ${status.replaceAll('_', ' ')}${reference}${verdict}`;
    }).join(' · ');
  }).join(' · ') || 'Evidence missing or unverified';
}

export function reviewDraft(review: Review) {
  const links = verifiedEvidenceLinks(review);
  return [
    `Review ${review.id} · ${reviewLabel(review)}`,
    `Question version: ${review.version || 'unknown'}`,
    `Question: ${review.question || 'Not available'}`,
    `Readiness: ${reviewReadiness(review)}`,
    links.length ? `Verified evidence: ${links.map((l) => `${l.kind}: ${l.url}`).join('; ')}` : `Evidence: ${evidenceSummary(review)}`,
    '',
    'My answer: ',
  ].join('\n');
}

export function prefillReviewAnswer(review: Review) {
  const current = useDrafts.getState().drafts.jevons || '';
  useDrafts.getState().setDraft('jevons', current ? `${current}\n\n${reviewDraft(review)}` : reviewDraft(review));
}

export function ReviewsList({ onOpen }: { onOpen: (id: string) => void }) {
  const query = useReviews();
  return <div className="reviews-list">
    {query.isPending ? <p role="status">Loading reviews…</p> : null}
    {query.isError ? <p role="alert">Reviews unavailable. <button onClick={() => void query.refetch()}>Retry</button></p> : null}
    {query.data?.length === 0 ? <p>No open reviews.</p> : null}
    {query.data?.map((review) => <button className="review-row" key={review.id} onClick={() => onOpen(review.id)}>
      <strong>{reviewLabel(review)}</strong>
      <span>{review.question || 'Question unavailable'} · v{review.version || '?'}</span>
      <small>{reviewReadiness(review)}</small>
    </button>)}
  </div>;
}

export function ReviewDetail({ id, onBack, onAnswer }: { id: string; onBack: () => void; onAnswer: () => void }) {
  const query = useQuery({ queryKey: ['review', id], queryFn: () => readReviews<Review>(`/api/reviews/${encodeURIComponent(id)}`), retry: false });
  const review = query.data;
  const links = review ? verifiedEvidenceLinks(review) : [];
  return <main className="review-detail">
    <button onClick={onBack}>← Reviews</button>
    {query.isPending ? <p role="status">Loading review…</p> : null}
    {query.isError ? <p role="alert">{query.error.message}</p> : null}
    {review ? <article>
      <h1>{reviewLabel(review)}</h1>
      <p>Review ID: <code>{review.id}</code> · Question ID: {review.ask_id || 'unknown'} · Version: {review.version || 'unknown'}</p>
      <h2>Question</h2><p>{review.question || 'Question unavailable'}</p>
      <p><strong>Readiness:</strong> {reviewReadiness(review)}</p>
      <p><strong>Target:</strong> {review.target_lookup === 'verified' ? review.target_status || 'status unavailable' : review.target_lookup || 'title/status unverified'}</p>
      <h2>Evidence</h2>
      {links.length ? <ul>{links.map((link, i) => <li key={`${link.kind}-${i}`}><a href={link.url} target="_blank" rel="noopener noreferrer">{link.kind}</a> (verified)</li>)}</ul>
        : <p>No verified fetchable evidence links.</p>}
      <p>{evidenceSummary(review)}</p>
      {review.resolution ? <p>Resolution: {review.resolution}</p> : null}
      <button type="button" onClick={() => { prefillReviewAnswer(review); onAnswer(); }}>Answer in chat</button>
      <p>Opens an editable draft in ordinary root chat. Nothing is sent or resolved by this button.</p>
    </article> : null}
  </main>;
}
