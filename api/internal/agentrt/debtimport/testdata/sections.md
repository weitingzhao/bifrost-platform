上次查看：92c0271（2026-10-10，Owner 已看过 TD-274 DEV 结果；本轮基线）

## 需要你拍板

### 现金写入失败要改成抛错吗？（改 core 公开接口）

- 推荐：B。core 新增严格版写入函数（失败抛错、返回写入与跳过数），旧函数保留一个版本；Flex 插件改调新函数，解析出行数 > 0 而写入 0 时任务失败。做法同第 1 轮 TD-80 C2-a。
- 选项：A：直接改 `upsert_account_transactions` 的语义并抬高下游下限 · B：新增严格版、旧版留一版 · C：只在插件侧判断（core 不动）
- 项：TD-91

### 让 CI 卡住发布吗？（跨仓库发版）

- 推荐：A。`release.sh stg/prod` 和 deliver-research / build-research-dagster 在该 SHA 的 CI 未成功时拒绝（Owner 可用 `--allow-red <理由>` 放行）；先修好 trade-api main 的红，三个插件仓库补上 CI 触发。
- 选项：A：发布前要求同 SHA 的 CI 成功 · B：deliver 流水线里先跑 lint-test 再构建 · C：维持现状，只在发布后告警
- 项：TD-95

## 数据边界（接受并留座）

> 订阅或市场本身没有的数据：接受，页面留座并写明原因；不算债、不编号，订阅或市场变化时重开（§5「缺数据先分原因」）。来自 Data Gaps 看板，10-06 并入。

- **已确认的财报日历（◉）** — 订阅里没有：10-06 `/research/events/calendar?days=60` 8 行，全是 ws:macro 与 ws:corporate，财报 0 行；插件只有 8-K Item 2.02 的过去财报。页面写 estimated · confirmed dates not in the subscription；预计日 expected_next 在跑（NVDA → 2026-11-18，track n=4、中位误差 0 天）。依据：看板 R2.a，10-06 实测。还在写「no earnings date reaches this side」的四处在 TD-150。
- **merger / spinoff 与 IB Flex 公司行为报表** — Owner 2026-09-17 裁定不做，除非重开：vendor 只给 dividend 与 split（抽样 17 名 833 行，0 merger、0 spinoff），能填的只有 Flex 的公司行为报表，没有接。依据：`bifrost-trade-frontend/src/layout/designNotes/portfolio.ts:76`；看板 B4.2-3。
- **CUE 只有调整合约** — CUE 1:30 合股（除权 2026-04-24）后交易所没有挂标准系列，只剩 10-16 到期的 14 个 CUE1 合约（10-05 OI 14 行）：市场本来没有，max pain / ATM IV / GEX / flow / PCR 留空是对的。依据：看板 R10.CUE，10-06 实测。页面上的说明已上线（frontend 8d991d8，原 TD-150，2026-10-08 签收）。

## 主题（第 2、3 轮）

- **Green while wrong: jobs succeed on zero, partial or garbage output** — The dominant round-2 class. Engines, gates, ingest handlers and writers convert failures into success: empty lists read as answers, exceptions become 0 or 'unknown', truncation is a field nobody checks, freshness bumps on zero-row jobs. Dagster and Flex show green; only manual metadata reading finds it. Fix pattern: raise or record reasons, and add output checks per asset. (TD-91, TD-92, TD-93, TD-94, TD-97, TD-100, TD-101, TD-106, TD-113, TD-116)

## 先看这几条

- **TD-91** — 现金流水写库失败被记成「成功、0 行」。TD-88 刚补完五个月的洞，这个写入方再静默失败一次，新的覆盖告警要到月底才响。

## 还债顺序

### 数据完整度执行清单（Owner 2026-10-10 确认）
| 顺序 / 已有债 | 状态与依赖 | 详细工作及交付物 | 验收与防线 | 执行边界 |
|---|---|---|---|---|
| 3 · TD-172 / TD-142 联合回填盘点 | 方案已确认；第一限定批已执行验收，整债未结 | `docs/audits/TD-172-142_GAP_PLAN_2026-10-10.md`：TD-172 725 × 59 session、TD-142 首批 23 × 252 session；已有来源模拟可增 756 个不同 ATM expiry 行，覆盖 606 symbol-day；条件重抓清单联合去重 66649 个合约区间 / 161863 个目标 contract-day；小试批 82 区间 / 120 目标日 | 7 项测试及只读全流程 PASS；合法零、整日缺失分母、Brent 双侧与 request 去重验证。SPY/NVDA/AAPL 仅本地重算不改善 90-day cone；GOOG 可达 104/74（90/180）session。受限 50–220 writer、cone180、Plugin long-breadth / Research valid-bracket 防线已在隔离分支补齐；尚未生产运行 | 已批准复用 756 键及 82 区间/120 目标日试批；保留现有有效短期行，禁止 long-only 输入触发整日 DELETE；Plugin 写 raw，Research 写 features；不使用 SVI 拼接；第 4 项限定批已实写验收，扩大/短腿仍另批 |

### 第 0 波 · 第 1 轮收尾

目标：第 1 轮剩下的三项按已排的日期收掉。TD-85 剩 Golden Source 的 PUBLIC CONNECT；TD-51 等 Loki 闸门后随下一次 Trade 发布；TD-80 C2-b 改号 core 0.49.0 与 TD-51 同发。

项：TD-85 · 已还：TD-21, TD-51, TD-80

### 第 1 波 · 正在出错的数据与「绿着的失败」

目标：先让失败变红。错数据先修数据（TD-87 restate），再把引擎、闸门、写入方从「出错也报成功」改成失败即失败：引擎资产按输出判定、husbandry gate 失败即关、日历读失败报错、写入方失败抛错。不需要 Owner 批的先做。

项：TD-91, TD-92, TD-94, TD-97, TD-101, TD-156, TD-189, TD-192, TD-244 · 已还：TD-88, TD-89, TD-90, TD-113, TD-93, TD-165, TD-167, TD-87, TD-245, TD-136, TD-157, TD-166

## 没覆盖到的（下一轮从这里开始）

- Per-engine numerical correctness beyond dates: GEX sign conventions, max-pain, VRP realized-vol windows, vanna/charm, SVI fit, flow maths

## 怎么做的

第 3 轮（2026-10-07，Ops 平台 · Console 与 MCP · Trade 前端数据正确性 · bifrost-ui 与 trade-worker · 数据层）：10 个 Agent，约 30 分钟，约 230 万 token，全程只读。代码读的是 origin/main 的干净副本：platform e95be35 · frontend dfb7858e · ui 2271260 · worker c04fc80 · infra f41dab5 · trade-api 630ca41 · core 53378bf。五个领域各一个盘点 Agent，三个反驳 Agent（平台两块合一个、前端与 ui/worker 合一个、数据层一个），一个 Agent 清点防线，最后一个去重排序。41 条发现：18 条原样成立，23 条改了说法或优先级，0 条被推翻；去重后 37 条，另 5 条并入已有条目或拆分；TD-196 加补充；关闭的 TD-140 原因写错，另开 TD-240；过时的登记文字汇成 TD-241。

## 条目

### TD-91

**P1 · flex-ib · A failed cash-transactions write is recorded as a successful run: core returns 0 on any exception and the job counts it as 'ok, 0 rows'**

- **状态**：观察中（core 0.58.0 + flex 0.13.0 均已上线 10-07；到 10-08 看 flex 现金流水作业结果）
- 审批 改公开接口 · 代价 S · 风险 low · repos: bifrost-trade-core, bifrost-platform-plugin-flex-query

### TD-95

**P2 · research-control · No release path is gated on CI: deliver-research ships SHAs whose CI is red (CI starts 7s after deliver), and release.sh never checks CI for Trade**

- **状态**：观察中（validate-git-sha 与 research-lint-test 已 apply 进 deliver-research / build-research-dagster；下一次 research 发版首跑）
- 审批 跨仓库发版 · 代价 S · 风险 low · repos: bifrost-trade-infra, bifrost-research

### TD-142

**P2 · research-data · The 90-day IV cone has 31–39 sessions of history and there is no 180-day tenor: ATM IV was stored only to 90 DTE before 2026-08-05**

- **状态**：在做（2026-10-10）：此前限定批及本次主64父键均实写/DEV验收PASS，三名90/180均≥60；未发布、整债未结，备用未用，追加范围待另批。历史验收及剩余深度/breadth见下列记录。
- 审批 本批已批准，扩大/短腿另批 · 代价 M · 风险 med · repos: bifrost-research

### TD-172

**P2 · research-data · ATM IV has almost no 50–90 DTE expiry from 2026-07-06 to 09-25 (the EOD chain stopped at the third listed expiry until plugin 0.39.0); the fix was forward-only, so term structure reads na for that stretch**

- **状态**：在做（2026-10-10）：此前限定批及本次主64父键均实写/DEV验收PASS，三名90/180均≥60；未发布、整债未结，备用未用，追加范围待另批。历史验收及剩余深度/breadth见下列记录。
- 审批 本批已批准，扩大/短腿另批 · 代价 M · 风险 low · repos: bifrost-platform-plugin-market-data, bifrost-research
