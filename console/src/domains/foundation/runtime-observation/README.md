# Runtime observation

Formal peer view: **Signals**, `#observation`, outside the Console shell.
Console has a Signals mode link; City and Agents get a shell-owned Signals link
without changes to their in-progress renderers.

Uses the existing City hardware and alert adapters and existing coordination
`useCommunication` hook. No RPC, ingestion, ACK, workflow inference or health
rules are introduced. Display switches never control acquisition. A pinned
snapshot includes communication projections atomically; evidence timestamps
remain source timestamps. Hardware readings refresh every 30 seconds, while the
shared communication hook owns its cursor and refresh lifecycle. Communication
is explicitly global; no hardware-to-agent relationship is fabricated.

Shared engine contract: `@bifrost/ui/city/observation`, ObservationSnapshot v1,
CityContent v2, registered main views and layers. Full-path hardware selection,
view, layer visibility and time are independent. Unknown freshness and failed
reads remain distinct. Earlier history cannot be reconstructed from this view.

Development fixtures live in `console/fixtures/observation`; they are not live
acceptance evidence or a production entry. Rollback: revert the additive route,
consumer and coordinated UI commit. No persisted backend state or migration.

## Signal village (UI 0.31.0)

The single-page village supersedes the hardware-only floor. It consumes existing
public reads: `/api/v1/catalog/status`, `/api/v1/delivery/pipelines`, each registered
`bifrost-ci-*` pipeline's `/runs`, and `/api/v1/data-husbandry`. No backend, RPC or
schema changes. The four domain judgments and data-lane verdicts are preserved,
not recomputed. CI counts cover retained records in the response's trailing
24 hours, never complete history. Failed sources remain independent and unknown.

A viewer-denied domain source shows its permission reason. No token is copied
from MCP and no privilege is widened. The 14 fallback domain slots are the catalog
baseline, not measured online domains. Signed-in access uses the existing auth.

The engine draws static City-kit buildings with all four regions on one page.
Hardware and operations poll every 30 seconds; communication retains its shared
hook. There is no game loop. Core facts remain visible; long explanations are
compact signs with full source text and timestamps in evidence. Selection and
pinning use the same ObservationSnapshot v1; no secondary source or score exists.

`observation-preview.html?scenario=village` is local recorded QA. It requires an
untracked `.observation-cache/domain-status-recorded.json` captured from a real
platform read. Other reads are captured once. RECORDED is never live acceptance.
Rollback this consumer commit and matching UI commit to restore the prior floor;
no stored backend data or migration needs reverting.

## World host integration (controlled Signals)

`useRuntimeObservationSnapshot()` from `useRuntimeObservationSnapshot.ts` exposes the
existing hardware + operations + communication composite as
`{snapshot, error}`. It mounts the original readers; there is no additional
ingest or history store. Keep its acquisition boundary mounted independently of
the display clock. Render a ready child once snapshot is available, and keep
that child's `useObservationContext(snapshot)` above navigation.

Pass its return value as `<RuntimeObservation observation={observation}/>`.
This controlled path mounts no readers and creates no private observation state.
Omitting the prop preserves the existing direct route and standalone behavior.
The UI entry `RuntimeWorld` also accepts the same observation value.

Selection, metrics, pin/resume and observation time flow through this one host.
Drawer/source-panel selection and alert visibility remain local; do not claim
these filters or source-panel IDs are restored by the Path context.
Scope/identity changes must remount/reset the host. Refresh cannot restore a
lost snapshot; show that limitation instead of treating current reads as history.

## Workload yard and database station (2026-11-23, UI / Signals)

`states.ts` adds two other lines' public reads to the same composite snapshot:
`GET /api/v1/bus/states?type=cluster.workload.changed|cluster.database.changed`
(communication line's bus ledger, v2: workload ready and version; database ready,
primary, backup, WAL archiving, last backup), polled every 60 seconds; and
`GET /api/v1/readings/evaluation?days=1&name=metric_coverage_by_namespace`
(metrics line readout, a closed day computed with every other reading on request),
read every 30 minutes. Workloads are grouped by namespace only; no domain mapping
is inferred. The bus lists only subjects that changed within its retention, and
the page says so. A failed read stays unknown with its reason. The database joins
the Data warehouse region; namespaces render as the UI's optional Workload yard
(@bifrost/ui 0.36.0); an older UI pin simply does not draw that region.
Rollback: revert this commit; no backend or stored state changes.
