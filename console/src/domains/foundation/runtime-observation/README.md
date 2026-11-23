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

The game presentation uses `RuntimeWorld` from @bifrost/ui 0.29.0. Measurements,
reported alert markers and communication evidence are independent display layers.
The scene sleeps when idle/hidden; subscriptions remain with the existing hooks.
Signal readings and evidence are real platform responses. The floor arrangement
is presentation, not physical location or asserted network links. Current scope
is hardware state, telemetry, reported alerts and shared communication coverage;
no business-payload internals or invented signal topology are introduced.
