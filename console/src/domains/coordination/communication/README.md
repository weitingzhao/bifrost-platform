# Agent collaboration adapter

Route: `#collaboration`, Crew navigation. Read-only observation.

## Sources and evidence mapping

- Existing `GET /api/v1/agentrt/events`: message_posted -> sent,
  message_acked -> acknowledged. Uses transaction cursor from the existing API
  client; first 500 events, incremental reads every 15 seconds, at most 1000
  retained events, visibly marked as bounded when a page/cap is reached.
- Existing bus contract and `useBusStream`: shared by City and collaboration.
  `foundation/bus/communication.ts` converts matching event/consumer receipts
  into delivered evidence. Console observer receipts are excluded. Event
  delivery is never presented as message delivery or ACK.
- Existing `GET /api/v1/agent/threads`: source-reported status and title,
  independently refreshed every 30 seconds. Does not infer work from mail.
- Crew public file `agent-config/charters/seats.yaml`: imported by
  `console/scripts/import-communication-roster.rb <source> <output>` into
  `roster.generated.json`. The generated artifact preserves its source hash,
  import timestamp, multiple memberships, temporary and outside seats. UI
  labels it as a source snapshot, not live presence. There is no invented
  runtime Crew endpoint. Re-run the importer when the source changes.

No message bodies or arbitrary mailbox reads are added. Current source metadata
supports sent and ACK for letters. Acceptance, linked reply, completion and Audit
remain not observed until an authoritative source supplies correlated evidence.
Unknown membership is visible. Authentication failures retain their error and
last-successful timestamp; they are not reported as zero activity.

## Ownership and overlapping files

New module: coordination/communication; generic model/renderer: bifrost-ui
src/communication. Public shared touchpoints: useBusStream (one reader with
subscriber lifecycle), bus/communication (receipt projection), City traffic.ts
(consumes that projection), shell routing and local package aliases. City layout,
buildings, technology tree and animation are unchanged. No other agent checkout
was edited. Changes are isolated in agent/communication-ui in both repos.

## Validation and limits

Model/public API tests cover independent stages, deduplication, channel separation,
unknown identity, multi-membership and time cutoff. Observer tests cover no network
writes. Existing City traffic and shell route tests run unchanged except adding the
new route expectation. Shared stream tests cover two observers and last-unmount.
Browser fixture: `/communication-preview.html`, explicitly illustrative, dev only;
it does not call Platform and is not a live integration acceptance result.

Live local entry is `http://127.0.0.1:5293/#collaboration`, bdev service
`communication-console`. This origin needs the existing Connect login. No tokens
were read or copied. Live authenticated black-box acceptance remains pending.
No deployment, remote push, or shared-checkout change was performed.

## Recovery

Both changesets are additive/local and separately reviewable. To remove the preview,
stop only `communication-console` via bdev. To withdraw the feature before merge,
exclude the two isolated branch changesets. After merge, revert both coordinated
commits together (UI public entry plus Platform consumer); no database migration,
platform message mutation, or persisted layout needs restoration.

## Decision record

Owner explicitly delegated reversible visual choice and implementation. Working
Studio was selected without another approval loop. The generic skill's request for
visual selection did not protect an irreversible action here; continuing with a
reviewable implementation removed that wait. No governance/CI gate was changed.
The real remaining credential step protects the Owner's secrets; the agent can
prepare and test the feature but cannot take over that credential under repository
rules. Visual and unit testing therefore continued independently of login.

## Peer modes

The default #collaboration route is now an independent AgentScreen outside the
Console frame. #collaboration?look=console uses WorkingStudio in ledgerOnly mode.
The optional exchange query parameter carries the selected communication identity
between them. AgentWorld consumes the same useCommunication snapshot and opens
member/record evidence on demand. City and Console navigation include Agents;
City's optional otherLooks prop is the sole renderer overlap for this revision.
No second mailbox, parser, identity resolver or transport is introduced.

## Letter round trips (2026-11-22)

All observation modes consume `@bifrost/ui/communication` snapshots via `useCommunication`; do not open another inbox or derive workflow in a renderer.

- `message_posted` -> `sent`; optional `reply_to` projects only a valid v1 envelope's declared original ID (new sends only). No body or summary is added to the ledger. It is not a claim that the sender completed anything.
- `seat.mail.arrived` -> `delivered` means persisted in the recipient mailbox, not delivered to a model context or opened.
- `message_acked` -> `acknowledged`, independent of delivery, reading, acceptance and completion.
- Shared engine links a reply only with explicit `replyTo`, an observed original and reversed participants. Replies remain independent letters with their own stages. No heuristic based on `answer`, prose, timing or names.
- `ReplyTrail` is shared by Agents and Console. It navigates original/replies and reports elapsed time at the last successful read. Missing reply evidence is not a timeout, demand for an answer, or an idle-agent verdict. Historical filters exclude future replies.
- `opened` is a supported evidence stage but this platform source does NOT currently record recipient body reads. MCP `read_message` can return a cached body without a platform write. Observer reads must not create read receipts. Acceptance/completion/audit likewise require explicit upstream records and remain unknown here.

Ownership overlap for the multi-view line: consume the public shared UI model and the host hook/adapter; no City geometry changes. Public optional additions are `CommunicationFact.replyTo`, `Exchange.replyTo/replies`, `Stage.opened` and `replyObservation`. The sender's envelope contract remains v1 with existing `reply_to`; event projection adds an optional field, preserving older events. No new permissions or inbox endpoints.
