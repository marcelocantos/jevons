-- Copyright 2026 Marcelo Cantos
-- SPDX-License-Identifier: Apache-2.0
-- One subscribed projection over the shared control replica. Observation
-- timestamps stay provider-owned; opening a view never changes them.
-- 600s = ResetsKeyBucket; 900s = DefaultStaleAfter; 80 = MaxHistoryPoints.
-- MATERIALIZED keeps correlated per-window JSON from rerunning history ranks.
WITH windows AS MATERIALIZED (
 SELECT p.provider, CAST(w.key AS INTEGER) AS ordinal, w.value AS value,
  CASE
   WHEN p.provider = 'cursor' AND lower(COALESCE(json_extract(w.value, '$.model'), '')) = 'api' THEN 'api'
   WHEN json_extract(w.value, '$.name') = 'weekly' AND
    (p.provider = 'cursor' OR json_extract(w.value, '$.limit_window') / 1000000000 BETWEEN 2160000 AND 3024000)
    AND NOT (COALESCE(json_extract(w.value, '$.limit_window'), 0) / 1000000000 BETWEEN 518400 AND 691200) THEN 'monthly'
   ELSE json_extract(w.value, '$.name') END AS name,
  COALESCE(json_extract(w.value, '$.model'), '') AS model,
  COALESCE(CAST(round(unixepoch(json_extract(w.value, '$.resets_at')) / 600.0) AS INTEGER), 0) AS period
 FROM claudia_plan_usage AS p
 JOIN json_each(p.reading_json, '$.windows') AS w ON TRUE
), history_bounds AS MATERIALIZED (
 -- Use the complete longest current period, not a fixed recent sample cap.
 -- Unknown reset/duration has no truthful lower bound: retain that provider's
 -- complete history. One reset bucket accommodates reset-time rounding/drift.
 SELECT w.provider, min(CASE
  WHEN w.period = 0 OR COALESCE(json_extract(w.value, '$.limit_window'), 0) <= 0 THEN ''
  ELSE strftime('%Y-%m-%dT%H:%M:%S', unixepoch(json_extract(w.value, '$.resets_at'))
   - json_extract(w.value, '$.limit_window') / 1000000000 - 600, 'unixepoch')
  END) AS first_observation
 FROM windows AS w
 GROUP BY w.provider
), history_candidates AS MATERIALIZED (
 SELECT h.provider, h.observed_at, h.reading_json
 FROM history_bounds AS b
 JOIN claudia_plan_usage_history AS h
  ON h.provider = b.provider AND h.observed_at >= b.first_observation
), history AS MATERIALIZED (
 SELECT h.provider,
  CASE WHEN h.provider = 'cursor' AND lower(COALESCE(json_extract(w.value, '$.model'), '')) = 'api' THEN 'api'
   WHEN json_extract(w.value, '$.name') = 'weekly' AND
    (h.provider = 'cursor' OR json_extract(w.value, '$.limit_window') / 1000000000 BETWEEN 2160000 AND 3024000)
    AND NOT (COALESCE(json_extract(w.value, '$.limit_window'), 0) / 1000000000 BETWEEN 518400 AND 691200) THEN 'monthly'
   ELSE json_extract(w.value, '$.name') END AS name,
  COALESCE(json_extract(w.value, '$.model'), '') AS model,
  COALESCE(CAST(round(unixepoch(json_extract(w.value, '$.resets_at')) / 600.0) AS INTEGER), 0) AS period,
  h.observed_at AS at, json_extract(w.value, '$.remaining_percent') AS remaining,
  json_extract(w.value, '$.band') AS band
 FROM history_candidates AS h
 JOIN json_each(h.reading_json, '$.windows') AS w ON TRUE
 WHERE json_extract(w.value, '$.remaining_percent') IS NOT NULL
 UNION ALL
 SELECT l.provider, l.window AS name, '' AS model,
  COALESCE(CAST(round(unixepoch(l.resets_key) / 600.0) AS INTEGER), 0) AS period,
  l.fetched_at AS at, l.remaining, NULL AS band
 FROM windows AS current
 JOIN jv_plan_history_legacy AS l
  ON l.provider = current.provider AND l.window = current.name
   AND l.resets_key = CASE WHEN current.period = 0 THEN '' ELSE strftime('%Y-%m-%dT%H:%M:%SZ', current.period * 600, 'unixepoch') END
 WHERE NOT EXISTS (
  SELECT 1 FROM claudia_plan_usage_history AS h
  WHERE h.provider = l.provider
   AND h.observed_at >= strftime('%Y-%m-%dT%H:%M:%S', l.fetched_at)
   AND h.observed_at < strftime('%Y-%m-%dT%H:%M:%S', l.fetched_at, '+1 second')
 )
), ranked_history AS MATERIALIZED (
 SELECT h.provider, h.name, h.model, h.period, h.at, h.remaining, h.band,
  row_number() OVER (PARTITION BY h.provider, h.name, h.model, h.period ORDER BY h.at) AS position,
  count(*) OVER (PARTITION BY h.provider, h.name, h.model, h.period) AS points
 FROM history AS h
 WHERE EXISTS (
  SELECT 1 FROM windows AS current
  WHERE current.provider = h.provider AND current.name = h.name
   AND (current.model = h.model OR h.model = '') AND current.period = h.period
 )
)
SELECT {
 at: COALESCE((SELECT max(attempted_at) FROM claudia_plan_usage), '0001-01-01T00:00:00Z'),
 pending: sqldeep_json(CASE WHEN EXISTS (SELECT 1 FROM claudia_plan_usage) THEN 'false' ELSE 'true' END),
 backends: (SELECT {
  provider: p.provider, status: p.status, reason: p.reason,
  plan_type: COALESCE(json_extract(p.reading_json, '$.plan_type'), ''),
  fetched_at: COALESCE(NULLIF(p.success_at, ''), '0001-01-01T00:00:00Z'),
  success_at: p.success_at, attempted_at: p.attempted_at, next_attempt_at: p.next_attempt_at,
  stale: sqldeep_json(CASE WHEN p.status <> 'available' OR unixepoch('now') - unixepoch(p.success_at) > 900
   OR EXISTS (SELECT 1 FROM windows AS w WHERE w.provider = p.provider AND unixepoch(json_extract(w.value, '$.resets_at')) < unixepoch('now'))
   THEN 'true' ELSE 'false' END),
  stale_at: strftime('%Y-%m-%dT%H:%M:%SZ', min(unixepoch(p.success_at) + 900,
   COALESCE((SELECT min(unixepoch(json_extract(w.value, '$.resets_at'))) FROM windows AS w WHERE w.provider = p.provider), unixepoch(p.success_at) + 900)), 'unixepoch'),
  age_seconds: max(0, COALESCE(unixepoch('now') - unixepoch(p.success_at), 0)),
  override: sqldeep_json(COALESCE(o.override_json, 'null')),
  windows: SELECT {
   name: w.name, model: w.model,
   remaining_percent: json_extract(w.value, '$.remaining_percent'),
   used_percent: json_extract(w.value, '$.used_percent'),
   resets_at: json_extract(w.value, '$.resets_at'),
   limit_window_seconds: json_extract(w.value, '$.limit_window') / 1000000000,
   band: CASE WHEN p.status = 'available' THEN COALESCE(json_extract(o.override_json, '$.band'), CASE WHEN w.name = 'session' AND json_extract(w.value, '$.band') IN ('under', 'locked') THEN 'ok' ELSE json_extract(w.value, '$.band') END, '') ELSE '' END,
   pressure: CASE WHEN p.status = 'available' THEN json_extract(w.value, '$.pressure') END,
   history: SELECT {
    at: h.at, remaining_percent: h.remaining,
    band: CASE WHEN h.position = h.points AND p.status = 'available'
     THEN COALESCE(json_extract(o.override_json, '$.band'), CASE WHEN h.name = 'session' AND h.band IN ('under', 'locked') THEN 'ok' ELSE h.band END) ELSE CASE WHEN h.name = 'session' AND h.band IN ('under', 'locked') THEN 'ok' ELSE h.band END END
   } FROM ranked_history AS h
   WHERE h.provider = w.provider AND h.name = w.name AND (h.model = w.model OR h.model = '') AND h.period = w.period
    AND (h.points <= 80 OR h.position = 1 OR h.position = h.points OR CAST((h.position - 1) * 79 / (h.points - 1) AS INTEGER) <> CAST((h.position - 2) * 79 / (h.points - 1) AS INTEGER))
   ORDER BY h.at
  } FROM windows AS w WHERE w.provider = p.provider ORDER BY w.ordinal,
  spend: CASE WHEN p.status = 'available' AND p.provider IN ('claude', 'codex', 'grok')
   AND EXISTS (SELECT 1 FROM windows AS w WHERE w.provider = p.provider AND w.model = '' AND w.name IN ('weekly', 'session') AND json_extract(w.value, '$.used_percent') >= 100)
   THEN CASE
    WHEN json_type(p.reading_json, '$.spend') IS NULL THEN {used_aud: 0, limit_aud: 0, unavailable_reason: 'spend block missing from usage reading'}
    WHEN COALESCE(json_extract(p.reading_json, '$.spend.enabled'), 0) = 0 AND COALESCE(json_extract(p.reading_json, '$.spend.limit_reached'), 0) = 0 THEN NULL
    WHEN upper(trim(COALESCE(json_extract(p.reading_json, '$.spend.used.currency'), ''))) <> 'AUD'
      OR upper(trim(COALESCE(json_extract(p.reading_json, '$.spend.limit.currency'), ''))) <> 'AUD'
     THEN {used_aud: 0, limit_aud: 0, unavailable_reason: 'spend money missing or currency not AUD', limit_reached: sqldeep_json(CASE WHEN json_extract(p.reading_json, '$.spend.limit_reached') THEN 'true' ELSE 'false' END)}
    ELSE {used_aud: json_extract(p.reading_json, '$.spend.used.amount_minor') / CAST('1e' || json_extract(p.reading_json, '$.spend.used.exponent') AS REAL),
     limit_aud: json_extract(p.reading_json, '$.spend.limit.amount_minor') / CAST('1e' || json_extract(p.reading_json, '$.spend.limit.exponent') AS REAL),
     limit_reached: sqldeep_json(CASE WHEN json_extract(p.reading_json, '$.spend.limit_reached') THEN 'true' ELSE 'false' END)}
   END ELSE NULL END
 } FROM claudia_plan_usage AS p
 LEFT JOIN jv_plan_overrides AS o ON o.provider = p.provider
 ORDER BY p.provider)
}
