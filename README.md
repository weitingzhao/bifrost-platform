# bifrost-platform

**Bifrost Ops Platform** — AI-native environment governance control plane for [Bifrost Trade](https://github.com/weitingzhao/bifrost-trade-infra).

Manage and monitor **Development** and **Production** environments through the **Bifrost Ops Console**. The trade monitoring UI (`bifrost-trade-frontend`) remains separate.

## Prerequisites

- **Go 1.22+** — `brew install go` (macOS Apple Silicon: `/opt/homebrew/bin/go`)
- **Node 20+** — for `console/` (`npm install` in `console/`)

## Quick start

```bash
# 0. API deps (first time)
cd api && go mod tidy && go test ./...
# Build binary: cd api && make build   (not bare `go build` — main is in cmd/platform-api)

# 1. Optional: copy env for Ops capabilities probe
cp .env.example .env

# 2. Console deps (first time)
cd console && npm install && cd ..

# 3. Start platform (frees :8780 / :5180 if occupied, then runs API + Console)
./scripts/run_platform.py
# or: make start

# Console: http://127.0.0.1:5180  (default tab: **Control Room**; CI/CD overview: **Delivery**)
# API:     http://127.0.0.1:8780/health
# Context: http://127.0.0.1:8780/api/v1/context
```

With the trade dev stack running (`cd ../bifrost-trade-infra && make dev`), the matrix should show green reachability for local nginx + APIs.

```bash
curl -s 'http://127.0.0.1:8780/api/v1/matrix?env=dev' | jq .
curl -s 'http://127.0.0.1:8780/api/v1/topology?env=prod' | jq .
```

Edit network layout in `config/topology.yaml` (hosts, edges, K3s roles). Console **Runtime Map** shows hardware + software with live status.

**Delivery** tab — CI/CD dual track (near-term Mac runner vs target GitOps). **Architecture** — governance catalogs + **Copy for LLM**; hardware/scope live view is on **Runtime Map**.

## Layout

```
api/       Go control-plane API (:8780)
console/   React Utility UI (:5180) — Dense UI token style
agent/     Node agent (Phase B placeholder)
mcp/       MCP tools (Phase P2 placeholder)
config/    environments.yaml + ops-context.yaml (spine) + topology.yaml
docs/      Staging notes + MkDocs (:8060) — see docs/STAGING.md; governance lives in console/src/lib/architecture/
```

## Design principles

- **Polyglot backend** (Go for platform API); **unified human UX** (Dense UI CSS tokens)
- **Read-only Phase 0** — connectivity + auth matrix (L0); no trade write paths
- **Probe, don't duplicate** — aggregate health from existing trade stack endpoints

## Related repos

| Repo | Role |
|------|------|
| [bifrost-trade-infra](../bifrost-trade-infra) | Business Compose, Goal docs |
| [bifrost-trade-frontend](../bifrost-trade-frontend) | Trade monitoring SPA |
| [bifrost-trade-api](../bifrost-trade-api) | Business APIs (probed read-only) |

See Ops Console → **Architecture → Blueprint** (includes AI Native Platform goals). Docs staging policy: [docs/STAGING.md](docs/STAGING.md).
