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
