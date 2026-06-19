# CLAUDE.md — bifrost-platform (Bifrost Ops Platform)

> **North star (终极目标)**: Ops Console → **Architecture → Blueprint** · `console/src/lib/architecture/blueprintCatalog.ts` · spine `config/ops-context.yaml` → `north_star` · decision **D6** · milestone **ops-ui-actuation**.

> AI-native **control plane** for Bifrost Trade dev/prod environments. AI Native Platform goal integrated into Blueprint § AI Native Platform.

与本项目用户的所有对话一律使用中文。

## 文档优先级

1. **代码** — `api/`、`console/`、`config/` 为行为与契约的 ground truth
2. **Ops Console UI** — Architecture 页（Vision / Blueprint / Roadmap / Milestones / Environments / K3s / Standards）由 catalog TS 驱动，唯一权威治理源
3. **Spine** — `GET /api/v1/context`

**本 repo 没有 `docs/` 目录** — 所有治理内容由 Architecture catalog（TypeScript）驱动，不使用独立文档站。

## 职责范围

本 repo 是 **环境治理平台**（控制面），与 `bifrost-trade-*` 业务栈（数据面）分离：

| 子目录 | 职责 |
|--------|------|
| `api/` | Go — 环境注册、连通性/权限矩阵探测、未来审计 API |
| `console/` | React **Bifrost Ops Console** :5180 — Control Room, Runtime, Program, Architecture |
| `agent/` | 未来：各节点 Go 探针 |
| `mcp/` | 未来：Platform MCP Tools（只读矩阵等） |
| `config/` | `environments.yaml`, **`ops-context.yaml`** (spine), `topology.yaml` |

**不包含**：交易 daemon、IB Operator、业务 API 实现。

## 边界与纪律

- **North star**：除重启 Ops Platform 外，所有运维操作经 Console + platform-api（脚本仅作 API 后端执行器）— 见 Architecture → **Blueprint**
- **L0 默认**：Phase 0 以只读探测为主；actuation 按 L1/L2 逐步落地（milestone `ops-ui-actuation`）
- **R-DV3**：平台 Agent 不得触发自动交易 Engine 写路径
- **聚合不复制**：探测 `bifrost-trade` 已有 `/health`、`/auth/capabilities`，不 import Python 业务包
- **Agent 模式**：见 Architecture → **Agent Protocol** · `agentProtocolCatalog.ts`

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
