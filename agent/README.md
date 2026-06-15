> **Docs staging** — future plan only. Authoritative cluster actuation: Ops Console → Architecture → Standards · `bifrost-platform/console/src/lib/architecture/standardsCatalog.ts`

# bifrost-platform agent (Phase B)

Lightweight **Go node agent** deployed on Linux mini-pcs, Mac Minis, and GPU server.

## Planned responsibilities

| Capability | Description |
|------------|-------------|
| Host facts | CPU, memory, disk, uptime |
| Docker read-only | Container list / health via local socket (no trade writes) |
| K8s watch | Node and pod status after K3s bootstrap |
| Heartbeat | Register with `platform-api` for discovery |

## Phase 0

Not implemented. Connectivity matrix probes run from **platform-api** centrally.

## Security

- Agent credentials are **separate** from trade Ops tokens
- No access to `ib:operator:cmd` or `daemon_control` write paths
