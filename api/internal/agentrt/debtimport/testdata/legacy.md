## 条目

### TD-91

**P1 · flex-ib · A failed cash-transactions write is recorded as a successful run: core returns 0 on any exception and the job counts it as 'ok, 0 rows'**

- **状态**：观察中（core 0.58.0 + flex 0.13.0 均已上线 10-07；到 10-08 看 flex 现金流水作业结果）
- **验收结果**：PASS（代码层）2026-10-07 Claude 10-07 复验：各仓库分支合并后门禁全绿（core 13463、worker 180、trade-api 1018、flex 160、market-data 1288、research 2132、ib-gateway 84、platform Go 56 包 + Console 801 + agent 25、ui 5）
- **Evidence**:
  - `bifrost-trade-core/src/bifrost_core/portfolio/reader/accounts.py:1417` — `logger.warning("upsert_account_transactions failed: %s", e)`
  - `bifrost-trade-core/src/bifrost_core/portfolio/reader/accounts.py:1413` — `return len(rows)`
  - `bifrost-platform-plugin-flex-query/src/bifrost_flex_query/orchestration/transactions.py:104` — `msg = f"Upserted {n} transaction(s) from {len(entries)} Flex account(s)."`
  - `bifrost-platform-plugin-flex-query/src/bifrost_flex_query/worker/handlers.py:30` — `inserted = int(data.get("count") or data.get("inserted") or 0)`
- 审批 改公开接口 · 代价 S · 风险 low · repos: bifrost-trade-core, bifrost-platform-plugin-flex-query
