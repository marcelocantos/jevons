#!/usr/bin/env python3
# Copyright 2026 Marcelo Cantos
# SPDX-License-Identifier: Apache-2.0
"""Fit plan utilisation against transcript token counts (🎯T651).

Providers do not publish how a subscription plan converts token usage
into plan consumption. This script estimates it: it joins the plan
utilisation series that claudia reads from each harness's ``/usage``
endpoint (persisted by jevons) against the per-request token counts
mnemo ingests from transcripts on disk, and fits non-negative weights.

    python3 scripts/plan-usage-fit.py
    python3 scripts/plan-usage-fit.py --from 2026-09-08 --to 2026-10-01

Read ``docs/analysis/plan-usage-token-model-2026-09-14.md`` before
trusting any number this prints. The 2026-09-14 baseline was 41% mean
absolute error on held-out blocks with unstable per-class weights, which
is NOT good enough to drive a budget. Re-run as blocks accumulate and
compare against that baseline.

Three things in here matter more than the arithmetic:

  * **Censoring.** Once ``remaining`` reaches 0 the provider cannot
    report further consumption, so an interval ending at 0 understates
    demand by an unknown amount. Those intervals are dropped; including
    them drags every weight toward zero.
  * **Quantisation.** Claude reports whole percent, so one interval's
    delta is ±1pp whatever the tokens in it. Small blocks therefore have
    labels that round toward zero, which is why the block-level fit
    exists alongside the interval-level one.
  * **Population.** The label is account-wide, so the features must be
    too. mnemo (all harnesses, from disk) matches it; claudia's brokered
    events do not, because they only cover agents claudia itself runs.
"""

import argparse
import datetime as dt
import os
import sqlite3
import sys

import numpy as np
from scipy.optimize import nnls

CLASSES = ["input", "output", "cache_read", "cache_write"]

# Utilisation is reported as whole percent by every harness except
# cursor, so a single interval carries at most this much quantisation
# error. Block totals are compared against it to decide whether a block
# carries enough signal to be worth fitting.
QUANTISATION_PP = 1.0

# A block must lose at least this many percentage points before its
# total is worth more than the quantisation noise inside it.
MIN_BLOCK_DRAWDOWN_PP = 2.0

# Condition number above which the token classes are not separable and
# per-class weights must not be reported as meaningful.
COND_SEPARABLE = 100.0

READINGS_DB = os.path.expanduser("~/.jevons/plan-usage-readings.db")
MNEMO_DB = os.path.expanduser("~/.mnemo/mnemo.db")


def parse_ts(s):
    return dt.datetime.fromisoformat(s.replace("Z", "+00:00"))


def load_readings(path, provider, window, lo, hi):
    """Utilisation series, grouped into blocks.

    ``resets_key`` carries the provider's ``resets_at`` verbatim and
    jitters by a second or so between polls, which fragments one logical
    block across two or three keys. Truncating to the minute rejoins
    them. Rows with an empty key are readings where nothing had been
    consumed and the provider scheduled no reset; they carry no signal.
    """
    con = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
    rows = con.execute(
        """SELECT substr(resets_key, 1, 16), fetched_at, remaining
           FROM plan_readings
           WHERE provider = ? AND window = ? AND TRIM(resets_key) <> ''
             AND fetched_at >= ? AND fetched_at < ?
           ORDER BY 1, 2""",
        (provider, window, lo, hi),
    ).fetchall()
    con.close()
    blocks = {}
    for blk, ts, remaining in rows:
        blocks.setdefault(blk, []).append((parse_ts(ts), float(remaining)))
    return blocks


def load_tokens(path, lo, hi):
    """Per-request token counts for the harness that bills this plan.

    Reads the materialised ``*_m`` columns on the base table, never
    ``entries_v``: the view's COALESCE is not sargable and turns this
    into a full scan of every row in the database (mnemo 🎯T179).
    """
    con = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
    rows = con.execute(
        """SELECT e.timestamp,
                  COALESCE(e.input_tokens_m, 0),
                  COALESCE(e.output_tokens_m, 0),
                  COALESCE(e.cache_read_tokens_m, 0),
                  COALESCE(e.cache_creation_tokens_m, 0)
           FROM entries e
           JOIN session_meta sm
             ON sm.session_id = e.session_id AND sm.source = 'claude'
           WHERE e.type = 'assistant'
             AND e.timestamp >= ? AND e.timestamp < ?
             AND e.model_m LIKE 'claude%'
           ORDER BY e.timestamp""",
        (lo, hi),
    ).fetchall()
    con.close()
    return [(parse_ts(r[0]), *r[1:]) for r in rows]


def build_intervals(tokens, blocks):
    """One row per consecutive pair of readings inside a block."""
    times = [t[0] for t in tokens]
    X, y, meta = [], [], []
    censored = rising = 0
    for blk, readings in blocks.items():
        for (t1, r1), (t2, r2) in zip(readings, readings[1:]):
            if r2 > r1:
                rising += 1  # block boundary or credit top-up, not usage
                continue
            if r2 == 0.0:
                censored += 1
                continue
            lo = np.searchsorted(times, t1, side="right")
            hi = np.searchsorted(times, t2, side="right")
            agg = [0, 0, 0, 0]
            for rec in tokens[lo:hi]:
                for k in range(4):
                    agg[k] += rec[1 + k]
            X.append(agg)
            y.append(r1 - r2)
            meta.append(blk)
    return np.array(X, float), np.array(y, float), meta, censored, rising


def report_fit(label, X, y, groups):
    """Fit, then hold out whole groups — intervals within a block are
    serially correlated, so a random split would leak."""
    print(f"\n--- {label} ---")
    if len(y) < len(CLASSES) * 2:
        print(f"  only {len(y)} observations; refusing to fit")
        return
    scale = X.max(axis=0)
    scale[scale == 0] = 1
    cond = float(np.linalg.cond(X / scale))
    print(f"  condition number (scaled): {cond:,.1f}", end="")
    print("" if cond <= COND_SEPARABLE else "   NOT SEPARABLE — weights are not attributable")

    w, _ = nnls(X, y)
    print("  weights, percentage points per 1M tokens:")
    for name, wi in zip(CLASSES, w):
        print(f"    {name:<12} {wi * 1e6:>12.4f}")

    uniq = sorted(set(groups))
    errs = []
    for held in uniq:
        tr = [i for i, g in enumerate(groups) if g != held]
        te = [i for i, g in enumerate(groups) if g == held]
        actual = float(y[te].sum())
        if actual < MIN_BLOCK_DRAWDOWN_PP or not tr:
            continue
        wt, _ = nnls(X[tr], y[tr])
        errs.append((held, actual, float((X[te] @ wt).sum())))
    if not errs:
        print("  no block carries enough drawdown to hold out")
        return
    ape = [abs(p - a) / a for _, a, p in errs]
    print(f"  leave-one-block-out over {len(errs)} blocks "
          f"(>= {MIN_BLOCK_DRAWDOWN_PP:g}pp drawdown):")
    for held, a, p in sorted(errs, key=lambda e: -e[1])[:10]:
        print(f"    {held}  actual {a:6.1f}pp  predicted {p:6.1f}pp  ({(p - a) / a * 100:+.0f}%)")
    print(f"  MAPE {np.mean(ape) * 100:.0f}%   median APE {np.median(ape) * 100:.0f}%")


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--provider", default="claude")
    ap.add_argument("--window", default="session")
    ap.add_argument("--from", dest="lo", default="2026-09-08")
    ap.add_argument("--to", dest="hi", default="2100-01-01")
    args = ap.parse_args()

    for path in (READINGS_DB, MNEMO_DB):
        if not os.path.exists(path):
            print(f"missing database: {path}", file=sys.stderr)
            return 1

    blocks = load_readings(READINGS_DB, args.provider, args.window, args.lo, args.hi)
    tokens = load_tokens(MNEMO_DB, args.lo, args.hi)
    X, y, meta, censored, rising = build_intervals(tokens, blocks)

    print(f"window            : {args.lo} .. {args.hi}  ({args.provider}/{args.window})")
    print(f"token records     : {len(tokens):,}")
    print(f"blocks            : {len(blocks)}")
    print(f"intervals kept    : {len(y)}  (dropped {censored} censored at 0, {rising} rising)")
    print(f"drawdown explained: {y.sum():.0f} pp")
    print(f"tokens joined     : {X.sum():,.0f}")
    if len(y) == 0:
        print("\nno usable intervals")
        return 1

    print("\n--- marginal correlation with delta-utilisation ---")
    for k, name in enumerate(CLASSES):
        if X[:, k].std() == 0:
            print(f"  {name:<12} constant in this sample; unidentifiable")
            continue
        print(f"  {name:<12} r = {np.corrcoef(X[:, k], y)[0, 1]:+.3f}"
              f"   total = {X[:, k].sum():>15,.0f}")

    report_fit("interval level", X, y, meta)

    # Block totals: the quantisation error stays +-1pp while the signal
    # grows to tens of pp, so a block's label is far better conditioned
    # than any single interval's.
    names = sorted(set(meta))
    idx = {n: i for i, n in enumerate(names)}
    BX = np.zeros((len(names), len(CLASSES)))
    BY = np.zeros(len(names))
    for row, yy, blk in zip(X, y, meta):
        BX[idx[blk]] += row
        BY[idx[blk]] += yy
    keep = BY > QUANTISATION_PP
    report_fit("block level", BX[keep], BY[keep], [n for n, k in zip(names, keep) if k])
    return 0


if __name__ == "__main__":
    sys.exit(main())
