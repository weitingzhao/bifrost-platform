# Agent collaboration observation

Routes: `#collaboration` (Agents), `#collaboration?look=console` (records). Both consume one shared communication snapshot. This is an observer; viewing, seeking and replay never send, ACK, accept or approve.

## Public integration contract

Host: `useCommunication()` returns `CommunicationSnapshot`; `useCommunicationObservation()` returns the same snapshot plus host-owned history controls. Generic engine: `@bifrost/ui/communication`. Consumers must reuse this adapter/query cache, not build a second mailbox reader, parser, identity resolver or status calculator.

Additive v1 model fields: `CommunicationFact.replyTo`, `Exchange.replyTo/replies`, `Stage.opened`, `Member.work`, `Member.records`. Work records and reports carry source IDs and timestamps. Optional work properties do not imply authorization. Platform vocabulary and identity joins remain in `adapter.ts`.

## Authoritative sources and limits

- Existing `GET /api/v1/agentrt/events`: `message_posted` -> sent, `seat.mail.arrived` -> persisted inbox delivery, `message_acked` -> ACK. None implies recipient opened, accepted or completed.
- Initial latest 500 events; forward cursor refresh every 15 seconds. Load earlier uses the existing `before_event_id` pagination. Both merge into one deduplicated cache, preserving the forward cursor. Up to 10,000 retained records; cap, unavailable history and read errors are explicit. No full-history claim beyond source retention.
- Optional `reply_to` on new `message_posted` events comes from a valid existing v1 envelope. Only an explicit original and reversed parties establish a reply. No body/summary disclosure, historical backfill or prose matching. Replies have independent stages.
- Shared bus contract and `useBusStream` feed event-delivery evidence; Console observer receipts are excluded. Event receipts are not letter ACKs. Earlier event receipt history is not fetched here.
- `GET /api/v1/agent/threads`: title, last heartbeat event, activity and task binding. It does not prove domain membership or authority. Recent threads (24 h), known roster members and communication participants are observable.
- `GET /api/v1/agentrt/seats`: latest self-reported goal, doing, waiting_on, next, progress/basis and declared charter. Uses existing `seat-statuses` query. Reports older than two hours are labelled older, not represented as fresh work. Parse errors retain their problem and do not show progress.
- `GET /api/v1/lineage` with 2-day window: linked commit records and release reach. Uses existing `lineage-lines,2` query. Exact identities only; vendor-session aliases must have a unique thread-source match. No display-name guessing. Source record link opens Commit Lineage.
- Crew memberships remain the generated source snapshot from `agent-config/charters/seats.yaml`, with import time/hash in `roster.generated.json`. Multiple memberships and temporary roles remain explicit; self-reported charter does not overwrite them.
- Effective permissions are not exposed by these sources, so the member panel says so. Opened, acceptance, completion and audit require independent upstream evidence. MCP cached body reads are not durable read receipts.

## Visual and time behavior

Agents extends City's existing `Draw`, `faces`, `HUES` and sprite serialization with a cartoon block workbench and member. No City geometry, buildings, animations or technology tree were changed. The scene focuses on the selected communication; directory entries expose primary department and all memberships in detail. Colors identify members, never health or permission.

The timeline supports live following, fixed-time inspection, previous/next evidence, replay and earlier letter history. Historical filtering excludes future stages and replies. Current work is explicitly not reconstructed in history; timestamped work records can be filtered. Missing replies do not establish idle agents or deadlines. Reduced-motion suppresses the finite evidence pulse.

## Ownership and acceptance

Owned files: platform `coordination/communication`, fixture, dev entry scan; UI `src/communication`. Existing City kit is imported without edits. Other views use the shared snapshot and retain their independent composition.

Validation covers source identity joins (including ambiguous aliases), self-report vs membership separation, no fabricated progress on malformed reports, time cutoff/future ACK, shared historical cursor and read-only interaction. Local browser fixture is `http://127.0.0.1:5293/communication-preview.html#agents`, managed by bdev `communication-console`, explicitly illustrative. Production route has no fixture injection. Live acceptance and release SHAs are recorded separately in delivery evidence, not inferred from fixture success.

## Recovery

Revert the coordinated UI and platform commits through the normal release path. No database migration or persisted layout change. Stop only this line's preview using bdev when no longer needed.

## Host-controlled observation time (additive)

AgentWorld and WorkingStudio accept optional `through` and `onThrough(at)` props. Omit `through` to retain the existing local timeline behavior; pass an ISO timestamp to filter evidence, or the empty string for live. With a controlled value, interaction requests a change through the callback; the host supplies the next value. The cutoff is not a captured snapshot and does not reconstruct past membership or work. Both renderers continue consuming the same CommunicationSnapshot; no extra reader or snapshot store is introduced.

The engine's `useObservationContext` currently accepts ObservationSnapshot and Path selection, not CommunicationSnapshot and exchange IDs. Do not cast one into the other. The world/engine owners can retain the appropriate snapshot above navigation and bridge explicit selection/time semantics when their shared contract supports communication. Current route integration retains exchange/look only; this additive renderer capability alone does not claim cross-route historical restoration.

Agents day/night follows the existing host `document.documentElement.dataset.opsTheme`; Console appearance remains its owner. There is no Agents preference storage or independent clock. City geometry and other view styling are not changed.
