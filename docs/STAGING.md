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
| Compose → K3s roadmap | `console/src/lib/architecture/roadmapCatalog.ts` · **Platform Roadmap** |
| K3s target topology & checkpoints | `console/src/lib/architecture/k3sArchitectureCatalog.ts` · **K3s Architecture** |
| K3s first-node deployment runbook | `console/src/lib/architecture/k3sBootstrapCatalog.ts` · **K3s Bootstrap** |
| Deploy decision chain & sign-off gates | `console/src/lib/architecture/deployMainlineCatalog.ts` · **Deploy Mainline** (Program) |
| AI Native Platform mission & boundaries | `console/src/lib/architecture/blueprintCatalog.ts` § AI Native Platform · **Blueprint** |

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

## Repo documentation ownership

| Repo | Scope | Authoritative source |
|------|-------|---------------------|
| **bifrost-platform** (control plane) | Environment governance, cluster architecture, release gates, platform roadmap, AI Ops goals, deploy decision chain | Console Architecture catalogs (`*Catalog.ts`) → rendered in Ops Console UI |
| **bifrost-trade-infra** (data plane / workloads) | Docker build handbook, migration sign-off checklists, 2C Session runbooks, business API cutover flows | MkDocs :8050 (`docs/*.md`) |

**Rule**: a document that describes *how the platform evolves* (environment, cluster, release process, AI ops strategy) belongs in `bifrost-platform` catalogs. A document that describes *how a specific trade workload is built, tested, or cut over* belongs in `bifrost-trade-infra/docs/`.

## Infra handbook

Trade workload runbooks and migration sign-offs: **[bifrost-trade-infra MkDocs :8050](http://127.0.0.1:8050/)** — separate repo. Content is promoted to Console Architecture only when Owner elevates a section to platform governance.
