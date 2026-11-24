# Shared observation access v1

Host entry: `@/domains/foundation/observation-access`.
`useHardwareObservation()` returns the standard TanStack query result; `data`
is the existing `ObservationSnapshot` v1 from `@bifrost/ui/city/observation`.
`hardwareObservationOptions()` supplies the same options for QueryClient reads.
No new RPC, dependency, engine type or backend permission is introduced.

Mount only for consumers that need hardware/monitoring alerts. Consumers share
one host QueryClient and the deployed Signals `runtime-observation/hardware`
key; simultaneous requests deduplicate. Fresh data is reused for 30 seconds,
active consumers poll every 30 seconds; normal focus/reconnect behavior applies.
A cached result is not a claim of health or continuous observation. Source
failures are evidence inside data (query success alone is not source success).
Use `readQuality(evidence, observationTime)` and preserve original timestamps.
Cancelled captures do not publish partial evidence. Last successful cached data
may remain while a new request is in progress; its expiry remains unchanged.

Render the SAME data object with existing `ObservationSurface` or `RuntimeWorld`.
Pin the snapshot, not a reconstructed collection of current values. Snapshot
capture spans several requests; it is not a backend transactional snapshot.
Identity uses `pathKey` with full paths, never label matching. Do not mutate data.
The host owns identity/session cache disposal. This module neither persists
credentials nor shares cache across tabs or QueryClients.

Communication remains `useCommunication` / `useCommunicationObservation` from
coordination: one source, its own cursor, retained history, deduplication and
recovery. Do not add a message reader here. Operations remains the existing
runtime-observation adapter; this hardware entry does not fetch CI/domain/data.

## Ownership and migration

This directory owns access composition. runtime-observation owns capture and
projection; coordination owns communication; Crowd owns City; bifrost-ui owns
portable models and renderers. No existing scene or menu is modified here.
Signals can replace its hardware useQuery with this hook without changing data.
Its existing useQuery already shares this cache key. City currently caches
`{hardware,units,at}` under another key: do NOT alias that key to this snapshot.
Its owner must migrate explicitly, preserving units and selected-object behavior.
Until that migration City and Signals still perform separate hardware reads.

Rollback: remove this additive directory and consumers' imports; no server state,
message ACK, action, database migration or health rule changes exist.

## Verified consumer slice (2026-11-22)

`observation-access.html` is an independent local acceptance entry, built with
`vite.observation-access.config.ts`. It mounts this hook and supplies its data to
both existing engine renderers, with a shared pin. The preview deliberately does
not request operations; the current Signals renderer shows those regions as
unknown. It is a wiring example, not a replacement production menu or completed
Console redesign. Launch only through a dedicated bdev session.

Real platform readback: 14 hardware objects. `utilities/mini-pc-a` had identical
source, evaluation, receipt and expiry fields in both renderers. The shared pin
retained capture `2026-11-23T03:07:00.216Z` while incoming advanced to
`2026-11-23T03:07:30.387Z`. These are recorded acceptance times, not current status.
Mocked transport tests cover one concurrent capture, remount cache reuse,
GET-only scope, failure recovery, unchanged pinned data, true zero, stale/unknown
time and cancellation. Browser testing uses real telemetry, not mocked messages.

## Shared observation context (UI 0.32.0)

`@bifrost/ui/city/observationContext` now provides `useObservationContext`.
It retains one optional pinned snapshot reference, full selected path, metrics
visibility and observation time. Keep it above navigation; pass the returned
snapshot/context to engine projections. World navigation owns where it lives;
Ledger owns precise reading projections. This host access directory still owns
only reading/caching, not UI state or a second copy of the hook.

The local acceptance entry uses this hook with the existing readingView and
statusView. Real capture 2026-11-23T03:40:06.532Z remained fixed at observation
time 03:40:13.393Z while incoming advanced to 03:40:37.034Z. Selecting another
real device from the second projection and returning from the first preserved
source evidence exactly. Resume used incoming data and retained the selection.
These are recorded acceptance facts, not current production state. Formal
Signals/Console/City controlled-prop adoption remains with each consumer owner.

## Independent domain status

`domainStatusObservationOptions()` and `useDomainStatusObservation()` opt in to
only GET `/api/v1/catalog/status`, using the existing catalog client and cache
key `['status','domain-status']`. The response is unchanged (including unknown
fields); backend overall, four lights, coverage and references remain authoritative.
No CI/data/mailbox reads occur. Concurrent observers deduplicate; fresh data is
reused for 60 seconds and active observers poll every 60 seconds, retry=false.
The old Console query shares the same raw cache shape; migrate consumers to the
factory to also share cancellation and the freshness policy. AbortSignal reaches
serverFetch and cancelled responses cannot publish.

The hook adds `evidence` separately from `data`: absent while initially pending,
error on failed reads even if old data remains, original source `at` retained.
The 90-second evidence expiry matches the existing Signals display window and
never changes domain health. Missing/invalid source time stays unknown; an old
source is not made fresh by a later HTTP response. Use existing `readQuality` at
the observation time. Both data and evidence must be retained together if pinned.
Scope/session cleanup remains the host's responsibility. This is not the Signals
operations projection and does not claim its existing reader has migrated.

The catalog status client now uses the existing host authedFetch so a configured
viewer/operator login is forwarded without engine credential handling. Real local
acceptance without a viewer login returned 401: both consumers displayed the
same error evidence and receipt time, never zero domains or a healthy state.
Successful domain payload and recovery assertions use mocked transport; a real
authenticated domain success remains for the host consumer acceptance.

## Through the UI engine (step 2, 2026-11-24, off everywhere)

`engineHardware.ts` reads the same hardware from `GET /engine/v1/hardware` (bifrost-engine) and turns the engine's
answer back into the captures the direct read makes, so the same `snapshotOf` yields the same snapshot
(`engineHardware.test.ts`). `ENGINE_HARDWARE` switches it per environment, keyed by self-health's `viewer_env`; it is
empty, so every environment reads straight from the platform, and the direct read stays the way back. Through the
engine is its own cache key. Turn an environment on only after the comparison ran there as the viewer and differed in
nothing (bifrost-engine `scripts/compare-hardware.ts`, plus the snapshot equality on the page). Every consumer of
`useHardwareObservation` (Signals, the hardware evidence page) follows the switch: they share one cache.
