# Can transcript tokens predict plan utilisation? (2026-09-14 baseline)

Status: analysis · Verdict: **signal is real, model is not yet usable** · Target: 🎯T651
Reproduce: `python3 scripts/plan-usage-fit.py`

## Question

Providers do not publish how a subscription plan converts token usage
into plan consumption. The owner asked whether, now that `/usage`
metrics are accumulating, a static analysis of the historical data —
correlated against JSONL transcript data at the same granularity — would
yield a useful predictive model.

It was run. This document records the method, the numbers, and why the
answer today is *not yet*, so that re-running it later is a comparison
rather than a fresh start.

## Where each half comes from

**claudia collects; jevons persists; mnemo supplies the features.**

- **Label.** claudia's `LoadPlanUsage` reads each harness's `/usage`
  endpoint — for Claude, `GET https://api.anthropic.com/api/oauth/usage`
  (`anthropic-beta: oauth-2025-04-20`, bearer from the
  `Claude Code-credentials` keychain item) — returning `five_hour` and
  `seven_day` as `{utilization, resets_at}`. jevons persists the samples
  into `plan_readings` (`~/.jevons/plan-usage-readings.db`), ~5-minute
  cadence.
- **Features.** mnemo's `entries`, which ingests Claude, Codex, Grok and
  Cursor transcripts from disk and materialises per-request
  input / output / cache-read / cache-write counts.

The feature source is mnemo rather than claudia's own brokered events,
and that choice is load-bearing. **claudia only observes agents claudia
runs.** The owner's own Claude Code sessions, and mnemo's compactor
spawning `claude` directly, emit no brokered event at all. The label is
account-wide, so brokered events can never cover its population, at any
configuration. mnemo reads from disk and does.

Verified clean: only `source='claude'` carries Claude models (grok
sessions run `grok-4.6`, cursor runs `grok-4.6`/`composer-2.5`), so the
Claude-plan label has no cross-harness contamination.

## The five `/usage` series

| series | blocks | resolution |
|---|---|---|
| claude/session (5h) | 35 | integer % |
| claude/weekly | 4 | integer % |
| codex/weekly | 3 | integer % |
| grok/weekly | 2 | integer % |
| cursor/monthly | 1 | **full float** |

Only claude/session has enough blocks to fit. **Cursor reports
fractional utilisation** — roughly 100× the resolution of the others —
which makes it the only harness where a *single request's* consumption is
measurable. Its token data in mnemo is currently all zeros, so the best
label sits on the worst features; closing that is the highest-value gap.

## Method

Within a block, each consecutive pair of readings gives a Δutilisation,
and the tokens timestamped in that interval give a feature vector. Fit
non-negative weights (NNLS). Three decisions matter more than the
arithmetic:

- **Censoring.** Once `remaining` reaches 0 the provider cannot report
  further consumption, so an interval ending at 0 understates demand by
  an unknown amount. 113 such intervals are dropped. Keeping them drags
  every weight toward zero.
- **Quantisation.** Claude reports whole percent, so one interval's delta
  is ±1pp whatever the tokens in it. Hence the block-level fit alongside
  the interval-level one: block totals keep the ±1pp error while the
  signal grows to tens of pp.
- **Holdout by block, not by row.** Intervals inside a block are serially
  correlated; a random split would leak and flatter the result.

`resets_key` jitters by ±1s between polls, fragmenting one logical block
across 2–3 keys — 37 raw keys are really 35 blocks. Truncate to the
minute before grouping.

## Results

7,614 Claude assistant records, 34 blocks, 739 usable intervals,
970,887,193 tokens, 500pp of drawdown.

**The signal is unambiguous.**

| token class | r with Δutilisation | volume |
|---|---|---|
| cache_write | **+0.930** | 158M |
| output | +0.789 | 7.2M |
| input | +0.723 | 85k |
| cache_read | +0.429 | 806M |

**But the model is not usable.**

| fit | condition number | MAPE | median APE |
|---|---|---|---|
| interval level | 6.6 | 51% | 44% |
| block level | 28.2 | 43% | 39% |

Weights are also unstable between the two aggregations — `input` moves
from 0 to 871 pp/1M, `output` from 9.04 to 0. NNLS is exploiting an
85k-token column with almost no variance. **Per-class attribution from
this data would be confident and wrong.**

## What the errors say

The residuals have structure, which is more useful than the headline:

- Large blocks (85–98pp): −26% to +13%.
- Tiny blocks (1–3pp): within a few percent.
- **The 5–32pp middle band: over-predicted by 37–101%.**

Consumption per token appears to *fall* as block volume rises. That is a
real phenomenon to explain, not noise, and it is where the error lives.

One finding survived both fits: **`cache_read` took weight exactly zero
in each**, despite being 806M of the 970M tokens. Tentatively, cache
reads barely count toward the plan — consistent with their ~10× price
discount. If that holds, the quantity to watch is not total tokens at
all, which would change what any budget UI should display.

## What would move it

In order of expected value:

1. **More blocks in the 5–50pp middle band**, where all the error is.
   Twenty blocks with ≥2pp drawdown is too few.
2. **A longer window to decorrelate cache_read from cache_write**
   (r = 0.576 at block level today).
3. **A model split.** Opus and Sonnet are pooled; they may weight
   differently, and the mix is lopsided enough that pooling could be
   hiding it.
4. **The 7-day window as a covariate**, in case the session and weekly
   meters interact.
5. **Cursor token capture**, to exploit the one fractional-resolution
   label.

## Recommendation

Do not wire this into budget enforcement. At 43% error it would fire
wrongly often enough to be ignored, which is worse than no model —
and an ignored budget alarm is how 🎯T137's incident started.

Keep collecting. The join is built and the script re-runs in seconds;
compare against this baseline in a month.
