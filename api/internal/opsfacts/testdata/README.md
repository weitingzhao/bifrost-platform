# System facts contract (W-31 / v3.6 batch 1)

`GET /api/v1/ops/facts/system` requires the existing viewer role. HTTP 200 means
that the projection was assembled; inspect each fact's `state` before rendering.
`system.json` and `system-degraded.json` are synthetic, executable contracts,
not a snapshot of production. The contract tests compare the full JSON shape.

Each fact has `as_of` (completion of that server read), `source` (name, method,
path), nullable `watermark`, `coverage`, `state`, optional safe `reason`,
`merged_sources`, and nullable `value`. A failed/missing read has no numeric
value. `empty` is reserved for a successfully read empty collection; an empty
HTTP 200 body is `error`. A false policy or unheld window is `ok` with a false
boolean, not an error or a missing measurement. Unknown coverage booleans and
limits are null, not false/zero. Snapshot completeness refers only to that
source's response, not the company or all history.

`health` reports the platform's own health fields, not cluster availability.
`alerts` counts current instances by state and severity and supplies symptoms,
responsibility, and current window context. The full Prometheus label set is
hashed for a stable alert ID. Identical label sets merge, firing taking priority
over pending; `raw_total` preserves the source count and each merged instance
records its source. These are not incident, noise, or historical-opening counts.

Responsibility precedence is explicit alert `owner`, then exact namespace and
service in current `service_inventory` (active Prometheus targets) with exactly
one known owner, otherwise `unknown`. Service ownership does not prove a root
cause, an assigned task, or an action only the Owner can take. Conflicting,
missing, or stale inventory ownership is not guessed. Window `held`/`holder`
is current context; `relation=unknown` means no causal/queue link has been
established. A lease expiry is not a planned release end time.

`release_policy.value.valid` is authoritative, including false. `signing_state`
is `unsigned` only for the source's explicit missing-signed-policy reason;
other invalid policies are `signed_invalid` if a signing date is supplied, or
`unknown`. Upstream free-text error bodies and policy failure details are not
copied to the response. Other signing states do not grant release authorization.

`availability_7d` exposes per-series `observed_ratio` from
`avg_over_time(up[7d])` and nullable sample counts from
`count_over_time(up[7d])`. All scraped services remain separate; there is no
arbitrary average across pods/environments or a chosen control-plane SLO target.
Until expected cadence, target lifetime, and coverage are established,
`availability`, `overall`, and coverage completeness stay null, state `unknown`.
Two successful samples with ratio 1 must never render as seven days at 100%.
Query evaluation timestamps are not raw-sample watermarks, so watermark is null.
The nested sample-count fact preserves errors/forbidden/empty independently.

Freshness thresholds: alerts and active-target snapshots 2 minutes; policy
verification 1 minute; metric query responses 2 minutes (without pretending
that their generated timestamp is a raw-sample watermark). Health and lease
reads are current synchronous reads without a source refresh timestamp; an
expired held lease is stale. Missing timestamps remain null. Stale payloads are
retained for evidence, never silently promoted to current success.

Calendar windows use `America/Chicago`, Monday-start weeks, and DST-aware
calendar arithmetic. Trailing seven days means 168 hours in UTC. Round-to-date
requires an explicit verified round baseline; quota weeks require their own
pool/reset baseline. UTC calendar days have their own type. These range kinds
are distinct and boundaries are start-inclusive/end-exclusive. This endpoint
only measures the trailing-seven-day range; today/R4 trends are not inferred.

The adapter calls a fixed allowlist of existing read handlers in-process, with
a request deadline and bounded response bodies. It neither sends loopback HTTP
requests nor stores a new fact table/cache. The projection does not consume
`telemetry/overview`. Console should display `unknown`/`error` reasons and offer
the source as evidence, without rebuilding backend conclusions from mock data.
