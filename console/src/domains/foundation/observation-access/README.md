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
