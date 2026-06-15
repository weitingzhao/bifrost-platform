# Docs staging policy

`bifrost-platform/docs/` is a **lightweight staging area**, not the authoritative source for platform governance or architecture standards.

## Priority (for AI Agents and Owner)

1. **Code** — implementation, routes, config, tests (ground truth for behavior)
2. **Ops Console UI** — Architecture tabs (code-driven, closest to production)
3. **Spine** — `config/ops-context.yaml` via `GET /api/v1/context`
4. **Docs (this site)** — draft notes only; may be outdated or deleted after review

## Authoritative Architecture (Console → Architecture)

| Topic | Source |
|-------|--------|
| North Star, system blueprint, design principles | `console/src/lib/architecture/blueprintCatalog.ts` · **Blueprint** |
| Trade probes, cluster actuation, observability layers | `console/src/lib/architecture/standardsCatalog.ts` · **Standards** |
| Agent modes, context packs, forbidden actions | `console/src/lib/architecture/agentProtocolCatalog.ts` · **Agent Protocol** |
| Hardware, flows, platform phases | `console/src/lib/environments-catalog.ts` · **Environments** |

Use **Copy Prompt for LLM** on each tab, or **Copy All for LLM** on any Architecture page.

## What belongs in Docs staging

- **Future / not-yet-shipped** plans (e.g. Agent Phase B, MCP Phase P2)
- **Session notes** written by Agents while implementing — Owner reviews and either:
  - **Integrates** into an Architecture catalog + UI section, or
  - **Deletes** if redundant with code/UI

## What must NOT stay in Docs

- Content already rendered in **Architecture UI** (delete the markdown; do not duplicate)
- Long-lived governance standards (belong in `*Catalog.ts` + UI)
- Implementation details that live in code (read the code instead)

## Infra handbook

Deployment, migration sign-off, K3s hardware roadmap: **[bifrost-trade-infra MkDocs :8050](http://127.0.0.1:8050/)** — separate repo; integrate into Console only when Owner promotes a section to Architecture.
