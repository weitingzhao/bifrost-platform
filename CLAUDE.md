# CLAUDE.md — bifrost-platform

> AI-native **control plane** for Bifrost Trade dev/prod environments. Strategic goal: [bifrost-trade-infra/Goal/AI_NATIVE_OPS_PLATFORM.md](../bifrost-trade-infra/Goal/AI_NATIVE_OPS_PLATFORM.md).

与本项目用户的所有对话一律使用中文。

## 职责范围

本 repo 是 **环境治理平台**（控制面），与 `bifrost-trade-*` 业务栈（数据面）分离：

| 子目录 | 职责 |
|--------|------|
| `api/` | Go — 环境注册、连通性/权限矩阵探测、未来审计 API |
| `console/` | React Utility UI — Dev/Prod 矩阵（Dense UI token 风格） |
| `agent/` | 未来：各节点 Go 探针 |
| `mcp/` | 未来：Platform MCP Tools（只读矩阵等） |
| `config/` | 环境注册表 `environments.yaml` |

**不包含**：交易 daemon、IB Operator、业务 API 实现。

## 边界与纪律

- **L0 默认**：Phase 0 仅只读探测；不实现 `daemon_control`、不连 `ib:operator:cmd`
- **R-DV3**：平台 Agent 不得触发自动交易 Engine 写路径
- **聚合不复制**：探测 `bifrost-trade` 已有 `/health`、`/auth/capabilities`，不 import Python 业务包

## 端口

| 服务 | 端口 |
|------|------|
| platform-api | 8780 |
| platform-console | 5180 |
| bifrost-trade-frontend | 5173（业务，独立） |

## 命令

```bash
# 首次需安装 Go：brew install go
cd api && go mod tidy && make build   # main 在 cmd/platform-api，勿在 api/ 根目录裸跑 go build

make start        # ./scripts/run_platform.py — frees ports, api + console
make dev-api      # Go API only
make dev-console  # Vite console only
make test         # go test + console type-check
```

## 依赖关系

```
bifrost-platform (本 repo)
  → 只读 HTTP/TCP 探测 bifrost-trade 栈（nginx / 9 APIs / PG / Redis）

bifrost-trade-infra
  → 业务 Compose / Nginx / Goal 文档

bifrost-trade-{api,worker,socket,frontend,...}
  → 被治理的 workload
```
