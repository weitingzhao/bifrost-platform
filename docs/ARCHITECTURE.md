# bifrost-platform Architecture

## Control plane vs data plane

```mermaid
flowchart TB
  subgraph platform [bifrost-platform]
    Console[console :5180]
    API[api Go :8780]
    MCP[mcp Phase P2]
    Agent[agent Phase B]
  end
  subgraph trade [bifrost-trade data plane]
    FE[trade-frontend :5173]
    APIs[9 FastAPI domains]
    Worker[daemon + celery]
    Socket[IB + Massive edge]
  end
  Console --> API
  MCP --> API
  Agent --> API
  API -->|"L0 probe only"| APIs
  API -->|"TCP"| PGRedis[PG + Redis]
  FE --> APIs
  Worker --> Socket
```

## Authorization levels

| Level | Platform behavior |
|-------|-------------------|
| L0 | Read-only probes (Phase 0 default) |
| L1 | Safe retries via trade Ops API (future) |
| L2 | Owner-confirmed changes (future) |
| forbidden | Trade write paths — never exposed to platform AI |

## Ports

| Service | Port |
|---------|------|
| platform-api | 8780 |
| platform-console | 5180 |
| bifrost-trade-frontend | 5173 |

## Configuration

Environment registry: [`config/environments.yaml`](../config/environments.yaml)

Optional Ops token env vars for capabilities probe — see [`.env.example`](../.env.example).

## Related

- [TRADE_CONTRACT.md](TRADE_CONTRACT.md)
- [bifrost-trade-infra Goal/AI_NATIVE_OPS_PLATFORM.md](../../bifrost-trade-infra/Goal/AI_NATIVE_OPS_PLATFORM.md)
