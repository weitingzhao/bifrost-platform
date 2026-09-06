# CLAUDE.md — bifrost-platform (Bifrost Ops Platform)

> **Console version**: `0.2.0` — Platform Release UI polish, quiet-success design, action-first layout.

> **North star (终极目标)**: Ops Console → **Governance → Blueprint** · `console/src/lib/architecture/blueprintCatalog.ts` · spine `config/ops-context.yaml` → `north_star` · decision **D6** · milestone **ops-ui-actuation**.

> AI-native **control plane** for Bifrost Trade dev/prod environments. AI Native Platform goal integrated into Blueprint § AI Native Platform.

与本项目用户对话一律使用中文回复（无论用户用何种语言提问）；UI 字符串与代码标识符使用 English。

## 工作区定位（2026-09-06）

| 项 | 值 |
|---|---|
| 域 / 载荷 | Ops 控制面（**Rocket**）· Flywheel B · spine 宿主（`config/ops-context.yaml`）· 永不了解 Greeks / IB 协议 / SEPA / straddles / daemon 策略 |
| 运行位置 | 本机 `bdev`：platform-api `:8780` + console `:5180`；K3s `bifrost-platform-{stg,prod}`（Argo 自动同步 infra main） |
| 发布链 | `bifrost-deliver-platform` / `bifrost-deliver-platform-prod`；MCP server 源码在 `mcp/`（6 个 bridge，viewer/operator 令牌分级） |
| 仓库可见性 | GitHub **PUBLIC**（12 个 repo 全部公开）—— `.env`、Secret YAML、dump、kubeconfig、账户内容永不入库 |
| 硬边界 | D10 交易执行冻结（BLOCKED）· D13 三域边界 · 平台/业务解耦（Flywheel A/B） |
| 事实基线 | `../AGENT_FACTS.md`（§8c 运行时与安全事实）· 规则 `../CLAUDE.md`（§8 Claude Code 运行配置） |

会话请在工作区根 `/stocks` 启动（加载治理层 hooks / auto mode / 共享记忆）；运行时与安全事实以 `../AGENT_FACTS.md` §8c 为准。

## 文档优先级

1. **代码** — `api/`、`console/`、`config/` 为行为与契约的 ground truth
2. **Ops Console UI** — Governance 页（Vision / Blueprint / Roadmap / Standards / Agent Protocol / MCP Contract / Design System / Briefing Reconciliation / AI Compute）由 catalog TS 驱动，唯一权威治理源
3. **Spine** — `GET /api/v1/context`

**本 repo 没有 `docs/` 目录** — 所有治理内容由 Governance catalog（TypeScript）驱动，不使用独立文档站。

## 职责范围

本 repo 是 **环境治理平台**（控制面），与 `bifrost-trade-*` 业务栈（数据面）分离：

| 子目录 | 职责 |
|--------|------|
| `api/` | Go — 环境注册、连通性/权限矩阵探测、未来审计 API |
| `console/` | React **Bifrost Ops Console** :5180 — sidebar Command Hierarchy: Seat (Mission Control) · Partner (Engineer) · Mission (Satellite + Rocket) · Support (Ground Systems + Subcontractors); Governance in User → Guides |
| `agent/` | 未来：各节点 Go 探针 |
| `mcp/` | 未来：Platform MCP Tools（只读矩阵等） |
| `config/` | `environments.yaml`, **`ops-context.yaml`** (spine), `topology.yaml` |

**不包含**：交易 daemon、IB Operator、业务 API 实现。

## 边界与纪律

- **North star**：除重启 Ops Platform 外，所有运维操作经 Console + platform-api（脚本仅作 API 后端执行器）— 见 Governance → **Blueprint**
- **L0 默认**：Phase 0 以只读探测为主；actuation 按 L1/L2 逐步落地（milestone `ops-ui-actuation`）
- **R-DV3**：平台 Agent 不得触发自动交易 Engine 写路径
- **聚合不复制**：探测 `bifrost-trade` 已有 `/health`、`/auth/capabilities`，不 import Python 业务包
- **Agent 模式**：见 Governance → **Agent Protocol** · `agentProtocolCatalog.ts`
- **Page chrome（全站）**：页身份只在 `ConsoleHeader` breadcrumb（`plane › page` + `?` help + optional `pageActions`）；Task Mode 用顶栏 `TaskModeCapsule`（禁止独立 Mode banner 行）；Trade/Mission 用 `OpsContextStrip`（PageShell 内 elevated 页顶条，非 sticky chrome；Mission OK 时紧凑一行）；页内禁止 `PageHeader` / `ConsolePageHeader`；filters/actions 用 `PageToolbar` — 见 Governance → Design System · `designSystemCatalog.ts`
- **Page composition（三幕结构）**：每个 Mission Control 页面遵循 **Verdict → Body → Actions**。Verdict 用 `OpsVerdictStrip`（永远可见：一句话裁决 + StatusLamp/DenseTag + 可选 actions/meta）；Body 用 OpsSection 折叠单元（异常展开、正常收起）；核心操作永远可发现（不藏在折叠详情里）。详见 `designSystemCatalog.ts` → `PAGE_COMPOSITION`

## 端口

| 服务 | 端口 |
|------|------|
| platform-api | 8780 |
| platform-console | 5180 |
| bifrost-trade-frontend | 5173（业务，独立） |

## Viewer env（Daily Ops Fleet Desk）

Seat 优先级：`OPS_VIEWER_ENV` →（仅 in-cluster）`clusters.yaml` `viewer_env` → `dev`。

- **本地 `make start`**：无 `KUBERNETES_SERVICE_HOST` → 默认 **DEV**（即使加载了钉 `viewer_env: prod` 的 clusters.yaml）。`.env.example` 建议 `OPS_VIEWER_ENV=dev`。
- **Prod 部署（in-cluster）**：DefaultCluster 钉 `viewer_env: prod`；也可用 `OPS_VIEWER_ENV=prod`。
- 本地模拟 Prod 座位：`OPS_VIEWER_ENV=prod`。
- 见 `.env.example` 与 Governance → Agent Protocol → `DAILY_OPS_FLEET_DESK.acceptance`（Q1–Q6）。

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
