# Runtime observation slice

Ownership: UI multi-view execution line. System adapter only; rendering remains in the shared UI engine. No City or communication-owned file is edited.

## Run

Build the adjacent UI checkout with `npm run build`, then use the dedicated bdev session `observation-console`. Its command runs Vite with `vite.observation.config.ts`, port 5294, a private cache and the existing platform API target. Do not start a second unmanaged server.

- `/observation-preview.html`: real topology, hardware PromQL and monitoring alerts; read-only.
- `/observation-preview.html?scenario=quality`: synthetic zero/missing/error/stale/unknown-time cases, no platform reads.
- `/observation-preview.html?scenario=communication`: synthetic input generated through the existing communication model, no communication transport. Enable Communication to inspect the stage evidence.

Build independently: `./node_modules/.bin/vite build --config vite.observation.config.ts`. Output is `dist-observation/`; host behind the existing same-origin read-only API routes. No deployment is performed by this command.

## Existing contracts consumed

`readHardware`, `hardwareDistrict`, `alertsRead`, `deviceKeys`, CityContent, Place, Meter and CardView remain the owners of their current semantics. The adapter wraps responses to retain source, receipt, query evaluation time and errors; it never changes the existing health rules. HTTP endpoints are existing GET topology, telemetry/promql and telemetry/alerts. No new RPC, backend, table, score, ACK or approval action.

## Checks

`npm run type-check`; `./node_modules/.bin/eslint src/observation-preview`; `./node_modules/.bin/vitest run --config vitest.observation.config.ts`.

To verify the other line's actual model without copying it, supply its read-only source path in `COMMUNICATION_MODEL`. Optionally set `OBSERVATION_PROBE_OUTPUT` to `console/public/observation/communication-probe.json` to regenerate the browser probe. These are test-only integration inputs; the runtime does not import another worktree. The checked-in probe is explicitly synthetic, not a captured real message.

## Limits and rollback

Live communication is not wired: its shared contract is still in the other line's uncommitted work. Contract composition is verified with its real model and synthetic facts. No second subscription is created. No selected Agent is inferred to be handling a machine.

The slice observes hardware, not business payload contents. Backend-provided alert rows may be unplaced if exact identity evidence is unavailable. Query evaluation time is not exporter scrape time. Source clocks can be ahead of the browser; the inspector reports Clock mismatch rather than asserting freshness. Refresh expiry is two 30-second polls, a display deadline, not a health threshold.

Only the current snapshot can be pinned; no historical range API is added. Backend collection continues independently of UI animation/layer state. The dedicated entry is not added to the production Console menu.

Rollback: `bdev stop observation-console`, then remove this branch's additive entry/config/modules or revert its commit. No backend or persisted system data is changed. Shared route, Vite and test configuration files stay untouched.
