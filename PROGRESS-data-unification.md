# PROGRESS — 业务数据碎片统一

**Goal:** 每个业务实体只有一个写入点，其余全部派生。  
**存储原则:** 声明式 YAML（git）· 运行时 JSON（`data/`，gitignore）· 校验网关 Go API · Phase A–D 默认不上 PostgreSQL；复访见 D-DU8。  
**完成日期:** 2026-07-14（Waves）· Phase C：2026-07-15 · Phase D 决策收口：2026-07-15

| Wave | Phase | Status | Date | Summary |
|------|-------|--------|------|---------|
| W1 | W1-P1 Lane → YAML + API | ✅ | 2026-07-14 | `config/lanes.yaml` + GET/POST `/api/v1/lanes`；Console catalog API 水合；New Lane → POST |
| W1 | W1-P2 Program Template → YAML | ✅ | 2026-07-14 | `config/programs/_templates.yaml` + GET `/programs/templates`；from-template 支持 `lane_id` |
| W1 | W1-P3 Session Job 存档 | ✅ | 2026-07-14 | `data/sessions/*.json` + GET/POST `/api/v1/sessions`；pack 强制头部；progress `session_id` **必填** 校验 |
| W1 | W1-P4 Program JSON ↔ Lane | ✅ | 2026-07-14 | `ProgramStateRecord.lane_id`；Board Lane 列 + `?lane_id=` 过滤 |
| W2 | W2-P1 Migration Wave → YAML | ✅ | 2026-07-14 | `config/migrate-waves/*.yaml`；Go 加载；Console `migrateWaves.generated.ts`；CI `--check` |
| W2 | W2-P2 Agent Task → YAML | ✅ | 2026-07-14 | `config/agent-tasks.yaml`；Go + `GET /api/v1/agent-tasks`；Console 水合 |
| W2 | W2-P3 Environment → YAML | ✅ | 2026-07-14 | 运行时唯一源 `environments.yaml`；`TRADE_ENVIRONMENTS` `@deprecated` + CI |
| W2 | W2-P4 MCP Tool → API | ✅ | 2026-07-14 | McpContract 注明 tools 权威为 GET `/api/v1/mcp/tools`；面板已消费 API |
| W3 | W3-P1 New Lane / New Program UX | ✅ | 2026-07-14 | New Lane POST API；from-template 传 `lane_id`（briefingLane） |
| W3 | W3-P2 Pack + MCP 校验贯通 | ✅ | 2026-07-14 | pack 头部 session/program/phase；progress **强制** session_id |
| W3 | W3-P3 Briefing ↔ Board 链接 | ✅ | 2026-07-14 | Session CTA「Open Board」→ `#delivery-board?lane_id=` |
| W4 | W4-P1 CI schema 校验 | ✅ | 2026-07-14 | `scripts/ci/check_data_unification_catalogs.sh` |
| A+ | Phase A 遗留收口 R1–R5 | ✅ | 2026-07-14 | session 强制 · agent-tasks API · env deprecate · waves --check · queue 文档锚定 |
| C | Phase C 执行防漂 | ✅ | 2026-07-15 | Console Copy/Launch → POST sessions + pack 注入；MCP `create_session` + `report_phase_progress(session_id)`；`done`+`verify_cmd` 需 `verify_passed`；SDK claim **defer** · **Owner Accept** |
| D | Phase D 可选统一存储 | ⏸ DEFERRED | 2026-07-15 | 决策收口：不上 PG，仍用 `data/*.json`；多机不同步未成痛点；复访门槛见 D-DU8 · **不实现 schema / 不改 store** |

## 战略 A–D 状态

```text
A 止血     ✅
B 体验     ✅
C 执行防漂 ✅ (Accept)
D 可选存储 ⏸ DEFERRED（D-DU1 / D-DU8）
```

## Decision Log

- D-DU1: **不上 PostgreSQL**（Owner 拍板；**战略 Phase A–D 全程有效** —— 不为管理台专开连接池）
- D-DU2: Lane catalog 唯一写入点 = `config/lanes.yaml` via `POST /api/v1/lanes`
- D-DU3: Program templates 唯一源 = `config/programs/_templates.yaml`
- D-DU4: Migrate waves 唯一源 = `config/migrate-waves/*.yaml`（TS 由 `scripts/generate_migrate_waves_ts.py` 派生；CI `--check`）
- D-DU5: Agent tasks 唯一源 = `config/agent-tasks.yaml`（Console via `GET /api/v1/agent-tasks`）
- D-DU6: `report_phase_progress` 要求 `session_id`（空则拒绝）
- D-DU7: phase 有非空 `verify_cmd` 时，`status=done|complete` 必须 `verify_passed=true`（API 不 exec shell；Agent 本机跑 verify 后自报）
- D-DU8: **Phase D defer + 复访触发** —— 仅当以下任一成立才重开「正式 store / DB」议题：(1) Program 运行时 **>100**；(2) **多机** `data/` JSON 不同步已成为明确运维痛点；(3) **跨实例 / 多人** 必须共享同一份 progress/session 真相。在此之前保持 YAML（git）+ `data/*.json` + Go 校验网关。

## Known complexity (not fragmentation)

- **Briefing queue projection** in `console/src/lib/briefing/workLanes.ts` (~550 lines: `buildQueueForLane` spine/matrix → lane queue) remains heavy but correct. This is derivation logic, not a second SSOT. Revisit only if queue items move to structured store. Not in Phase A residual scope.
- **SDK claim job**（Portal Send to Cursor）仍 defer — 属 Phase C 遗留，非 Phase D。
- **Runtime fact store（Phase D 维持）:** `data/sessions/*.json` · `data/programs`（及邻域 runtime JSON）+ Go API 校验；**多机同步未立项**。不必先上 DB 也能消逻辑断层（A–C 已收口）。

## 验证

- `cd api && go test ./internal/lanes/ ./internal/sessions/ ./internal/devagent/ ./internal/migratewave/ ./internal/agentgovernance/ ./internal/opscontext/`
- `cd console && npx tsc --noEmit`
- `./scripts/ci/check_data_unification_catalogs.sh`
- `python3 scripts/generate_migrate_waves_ts.py --check`
- Phase D：仅文档；无 DB 相关新代码，无需为 D 单跑测试。
