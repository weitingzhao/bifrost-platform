# Bifrost Ops Platform — Docs (staging)

This MkDocs site is a **staging area** for draft notes and not-yet-shipped plans.

**Do not treat these pages as authoritative** if the same topic exists in code or Ops Console.

## Where to look first

| Need | Authoritative source |
|------|---------------------|
| North Star, blueprint, design principles | Ops Console → **Architecture → Blueprint** · `console/src/lib/architecture/blueprintCatalog.ts` |
| Probe contract, cluster actuation | Ops Console → **Architecture → Standards** · `standardsCatalog.ts` |
| Agent modes & context packs | Ops Console → **Architecture → Agent Protocol** · `agentProtocolCatalog.ts` |
| Environments, hardware, flows | Ops Console → **Architecture → Environments** · `environments-catalog.ts` |
| Live milestones & focus | `GET /api/v1/context` · Program / Control Room |
| API behavior | `api/` source · platform-api routes |
| Staging policy | [STAGING.md](STAGING.md) |

**Priority:** code → Console UI → spine YAML → docs staging.

## Run the console

```bash
make start    # platform-api :8780 + console :5180
```

Console sidebar: **Architecture** (governance) · **Docs** (this MkDocs site + infra handbook links).

## Infra handbook (separate repo)

**[http://127.0.0.1:8050/](http://127.0.0.1:8050/)** — `bifrost-trade-infra` — compose, migration, Goal, K3s roadmap.

## Staging pages (this site)

| Page | Status |
|------|--------|
| [Staging policy](STAGING.md) | Active — read before adding docs |
| [Overview](overview.md) | Repo README symlink |
| [Agent (Phase B)](../agent/README.md) | Future — not in UI yet |
| [MCP (Phase P2)](../mcp/README.md) | Future — not in UI yet |

Governance markdown (North Star, Architecture, Trade Contract, Cluster Actuation, Agent Modes) was **removed** — content lives in Console Architecture catalogs only.
