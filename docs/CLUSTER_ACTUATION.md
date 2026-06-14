# Cluster Actuation Plan

> North star: [NORTH_STAR.md](NORTH_STAR.md) — Strategy C hybrid control plane. All routine K3s operations go through Ops Console + `platform-api`; the only Owner exception is restarting Ops Platform itself.

## Scope

This document breaks cluster control into P1-P4 so the Platform can move from L0 observation to audited L1/L2 actuation without exposing arbitrary shell or raw `kubectl` as an operator surface.

## Feature Matrix

| Phase | Nodes | Workloads | GitOps / Delivery | Stack | Audit / Jobs |
|-------|-------|-----------|-------------------|-------|--------------|
| P1 | Read-only node health + **Layer A KPIs** (capacity, metrics-server usage when installed) | Ensure Bifrost namespaces, restart Deployment, scale Deployment, delete Pod, tail Pod logs | Documented only | Documented only | Bearer token auth, in-memory/JSON audit log, simple job list stub |
| P2 | Join node, drain/uncordon node, cordon node with Owner confirmation | Safer workload presets and namespace guardrails | Delivery page deep links | Preflight checks for platform add-ons | Durable jobs for long-running node operations |
| P3 | Node readiness gates for promotion | Workload rollout status gates | Argo CD sync/rollback, Tekton pipeline run and logs | GitOps app status | Shared audit contract for UI, API, future MCP |
| P4 | Cluster maintenance playbooks | Stack-wide restart presets | Release gate integration | Install/upgrade Platform stack add-ons via curated Helm/API presets | Persistent audit store, retention, export, job replay context |

## API Routes

### P1

| Method | Route | Role | Purpose |
|--------|-------|------|---------|
| GET | `/api/v1/auth/capabilities` | viewer | Report current Bearer token role and write capability |
| GET | `/api/v1/audit` | viewer | Return recent actuation records |
| GET | `/api/v1/jobs` | viewer | Return current simple job list |
| POST | `/api/v1/cluster/namespaces/ensure-bifrost` | operator | Idempotently create namespaces listed in `config/clusters.yaml` |
| POST | `/api/v1/cluster/workloads/rollout-restart` | operator | Rollout restart a Deployment with `{namespace, kind, name}` |
| POST | `/api/v1/cluster/workloads/scale` | operator | Scale a Deployment with `{namespace, kind, name, replicas}` |
| DELETE | `/api/v1/cluster/workloads/pods/{namespace}/{name}` | operator | Delete a Pod; controller may recreate it |
| GET | `/api/v1/cluster/workloads/pods/{namespace}/{name}/logs` | viewer | Tail Pod logs, with `tailLines` and optional `container` |
| GET | `/api/v1/cluster/metrics` | viewer | Cluster CPU/Mem usage (metrics-server), top pods in Bifrost namespaces (`?limit=8`) |

### P1.5 — Layer A observability (implemented)

Operational dashboards on the Cluster page — **not** full Prometheus/Grafana (Layer B).

| Surface | Data source | Notes |
|---------|-------------|-------|
| Overview KPI strip | `GET /cluster` + `GET /cluster/metrics` | Nodes ready %, running/pending pods, failing pods, metrics-server status, cluster allocatable CPU/mem |
| Nodes table | `GET /cluster/nodes` | Per-node allocatable CPU/mem/storage; CPU/Mem % when metrics-server is reachable |
| Top pods table | `GET /cluster/metrics` | Top N pods by CPU in `clusters.yaml` `bifrost_namespaces` only |

**Usage thresholds (lamp):** CPU/Mem ≥85% degraded, ≥95% fail.

**metrics-server:** Required for live usage % and top pods. Install on K3s when ready:

```bash
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
```

On some K3s dev clusters you may need `--kubelet-insecure-tls` on the metrics-server deployment.

**Layer B (deferred):** `monitoring` namespace — Prometheus, Grafana, Loki, node_exporter disk %, historical charts, alert rules. Cluster page will link/embed Grafana in a later phase; do not duplicate full monitoring in Console.

### P2

| Method | Route | Role | Purpose |
|--------|-------|------|---------|
| POST | `/api/v1/cluster/nodes/join` | admin | Start whitelisted K3s join job |
| POST | `/api/v1/cluster/nodes/{name}/cordon` | operator | Prevent new workloads on node |
| POST | `/api/v1/cluster/nodes/{name}/drain` | admin | Drain node with confirmation and job tracking |
| POST | `/api/v1/cluster/nodes/{name}/uncordon` | operator | Re-enable scheduling |

### P3

| Method | Route | Role | Purpose |
|--------|-------|------|---------|
| GET | `/api/v1/gitops/apps` | viewer | List Argo CD app health and sync status |
| POST | `/api/v1/gitops/apps/{name}/sync` | operator | Trigger Argo CD sync |
| POST | `/api/v1/gitops/apps/{name}/rollback` | admin | Roll back to approved revision |
| POST | `/api/v1/delivery/pipelines/{name}/runs` | operator | Start curated Tekton pipeline |
| GET | `/api/v1/delivery/runs/{id}/logs` | viewer | Stream or tail pipeline logs |

### P4

| Method | Route | Role | Purpose |
|--------|-------|------|---------|
| GET | `/api/v1/stack/addons` | viewer | Show Platform add-on install/upgrade status |
| POST | `/api/v1/stack/addons/{name}/install` | admin | Install curated add-on |
| POST | `/api/v1/stack/addons/{name}/upgrade` | admin | Upgrade curated add-on |
| POST | `/api/v1/promote/release-gate` | admin | Run release gate through Platform API |

## UI Structure

Cluster page evolves into tabs while preserving the current P0 sections:

| Tab | Phase | Contents |
|-----|-------|----------|
| Overview | P0/P1/P1.5 | KPI strip (reachability, pods, metrics-server, allocatable), kubeconfig sync |
| Nodes | P0/P1.5/P2 | Node table with capacity + usage %, readiness; future cordon/drain/join |
| Namespaces | P0/P1 | Bifrost namespace filter, Ensure Bifrost namespaces action |
| Workloads | P0/P1 | Deployment and Pod table, restart/scale/delete actions, Pod drawer |
| Logs | P1 | Pod drawer log tail and recent events |
| Metrics | P1.5 | Top pods (Bifrost NS), cluster usage summary via metrics-server |
| Audit | P1+ | Recent actuation records and future job links |
| GitOps | P3 | Argo CD apps, sync/rollback, Tekton runs |
| Stack | P4 | Platform add-on install/upgrade workflows |

## Guardrails

- P1 write routes require an operator Bearer token from `config/platform-auth.yaml` or env override.
- P1 supports only Deployment restart/scale and Pod delete; node join/drain and Argo/Tekton are deferred.
- API uses client-go directly for Kubernetes actions. Shell scripts remain implementation details only when a later phase explicitly whitelists them.
- Every write action records `actor`, `role`, `action`, `target`, `status`, and `detail` for LLM-ready audit context.

## Deferred Beyond P1

- Node lifecycle jobs and SSH executors: P2.
- Argo CD and Tekton integration: P3.
- Platform add-on installation and release gate execution: P4.
- MCP actuation tools mirror the same auth/audit contract after UI/API behavior is stable.
