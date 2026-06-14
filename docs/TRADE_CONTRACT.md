# Trade stack read-only contract

`bifrost-platform` **aggregates** health from the existing Bifrost Trade stack. It does not embed `bifrost-trade-api` or change response shapes.

## HTTP probes (via nginx)

Aligned with `bifrost-trade-infra/scripts/check_prod_stack.sh`:

| Target ID | Path | OK codes |
|-----------|------|----------|
| nginx-spa | `/` | 200 |
| api-monitor | `/api/monitor/status` | 200, 503 |
| api-massive | `/api/massive/research/massive/health` | 200, 503 |
| api-docs | `/api/docs/research/docs/health` | 200, 503 |
| api-ops | `/api/ops/health` | 200, 503 |
| api-trading | `/api/trading/health` | 200, 503 |
| api-strategy | `/api/strategy/health` | 200, 503 |
| api-portfolio | `/api/portfolio/health` | 200, 503 |
| api-market | `/api/market/health` | 200, 503 |
| api-research | `/api/research/health` | 200, 503 |

## Auth probe

| Target ID | Path | Token |
|-----------|------|-------|
| ops-capabilities | `/api/ops/ops/auth/capabilities` | `BIFROST_{DEV,PROD}_OPS_TOKEN` optional |

Response shape matches `bifrost_api.ops.auth` capabilities payload (`identity`, `capabilities.can_operate`).

## TCP probes

| Target ID | Address source |
|-----------|----------------|
| postgres | `environments.yaml` postgres host:port |
| redis | `environments.yaml` redis host:port |

## Policy-blocked rows (always shown)

| Target ID | Reason |
|-----------|--------|
| ib-operator-rpc | Trade write path — R-DV3 |
| daemon-control-write | Platform L0 does not invoke control writes |

## Topology API

`GET /api/v1/topology?env=dev|prod` merges:

- Static graph: `config/topology.yaml` (nodes, edges, Compose/K3s roles)
- Dynamic status: same probes as `/api/v1/matrix` for the selected environment

## Context API (spine)

`GET /api/v1/context` returns `config/ops-context.yaml` as JSON:

- `focus`, `deployment`, `milestones`, `decisions`
- `promotion`, `environments_extended`, `probe_hints`

Used by Ops Console Program / Pulse / Promote views and Copy for LLM.

Edit `topology.yaml` when hardware or K3s rollout changes; set `deployment_phase` to `compose` | `k3s_partial` | `k3s_ha`.

## Cluster API (K3s L0)

Platform API reads **local kubeconfig** (`PLATFORM_KUBECONFIG`, default `~/.kube/bifrost-k3s.yaml`). Console calls platform-api only — not the Kubernetes API directly.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/cluster` | Summary: `reachability`, `server_version`, `nodes_ready`/`nodes_total`, `failing_pods` |
| GET | `/api/v1/cluster/nodes` | Node list (name, status, roles, version, internal IP) |
| GET | `/api/v1/cluster/namespaces` | All namespaces; `?watch=bifrost` → `clusters.yaml` bifrost_namespaces |
| GET | `/api/v1/cluster/workloads?ns=` | Pod workloads in namespace |
| GET | `/api/v1/cluster/events?ns=&limit=` | Recent events (default limit 50) |
| POST | `/api/v1/cluster/sync-kubeconfig` | `{ok, path, message}` — runs infra `fetch-kubeconfig.sh` when sync enabled |

**Failure behavior:** missing kubeconfig or API unreachable → HTTP 200 with `reachability: fail` and `detail` (same as matrix probes).

**Env:** `PLATFORM_KUBECONFIG`, `PLATFORM_CLUSTER_SYNC_ENABLED`, `PLATFORM_CLUSTER_SYNC_SCRIPT` — see [`.env.example`](../.env.example).

**Authorization:** Cluster endpoints are L0 read-only. L1/L2 cluster actions are not exposed in Phase 0.

## Versioning

Phase 0: informal contract documented here. Breaking changes to trade health URLs must update `api/internal/probe/probe.go` and this file together.
