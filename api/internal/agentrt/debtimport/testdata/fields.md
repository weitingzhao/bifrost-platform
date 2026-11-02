## 条目

### TD-268

**P3 · ops-platform · `PrimarySQLHelper` is dead code: TD-256 and TD-259 took its last caller, and the ratchet that counted call sites now guards a function nobody calls**

- **状态**：待你签收（2026-10-19 21:4x；运行时 PROD M-9 收下、统筹线程手工合进 main，TD-383）
- **Claim**: After TD-256 (286c305) and TD-259 (a7ecb08) moved both plugin freshness probes onto plugin HTTP, `api/internal/cluster/pod_exec.go:65` `PrimarySQLHelper` has **no non-test caller**. What remains is the definition plus two test files that exist only to constrain it: `execsql_callers_test.go` (the LANE-P2 ratchet, baseline 2 → 0, now satisfied) and `pod_exec_live_test.go`. A ratchet counting call sites of a function with no call sites protects nothing.
- **Fix**: Delete `PrimarySQLHelper` and the two tests that exist only for it, and replace the call-site ratchet with one asserting **zero** references to the symbol anywhere outside its own removal test. **Do not narrow PROD `pods/exec` in `data`**: `execOnPrimary` is used throughout `data_clone.go` / `data_clone_fk.go`, and `execOnMinio` by `postgres_wal_repair.go` — both verified still in use on 634e305.
- 验收: `cd bifrost-platform/api && go test ./internal/cluster -run TestRemovedPrimarySQLHelperStaysGone` passes and `git grep -c PrimarySQLHelper origin/main` prints nothing.
- 审批 不用批 · 代价 S · 风险 low · repos: bifrost-platform

### TD-92

**P2 · research-control · Output checks execute and retain reasons, but ERROR alert receipt and persistent input gaps remain to be verified**

- **交付状态 2026-10-11**：已合入／未发布；最新 main 整合测试及 DEV 验收 PASS。对应提交和部署边界见 `docs/audits/DATA_INTEGRITY_MAIN_INTEGRATION_2026-10-11.md`；生产验收、告警送达、实际恢复及剩余数据缺口按各项条件保留，未据源码合入结债。
- **状态**：观察中（2026-10-10 三批真实运行及测试已复验；到下一次真实 ERROR 检查事件，看发送及接收回执；当前仍有持续输入缺口 WARN）
- **验收结果**：部分 PASS 2026-10-10 Research d628fa6：生产 0.205.0-dagster 对应模块 hash 相同；10-08/09/10 各 17 个引擎检查，14 PASS / 3 WARN。相关测试 56 passed / 0 skipped；合法零/异常零/跳过/失败原因有覆盖。10-07 实际有四个 ERROR 检查且 sensor tick 执行，但投递回执未找到，不关闭此债。报告：`docs/audits/TD-92-94_RUNTIME_ACCEPTANCE_2026-10-10.md`。
