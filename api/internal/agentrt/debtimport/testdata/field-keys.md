## 条目

### TD-152

**P2 · ops-platform · promtail drops log lines (ingester_error) around 02:00–03:15 and 22:xx UTC, so every Loki-based release gate can come out INCONCLUSIVE**

- **状态**：观察中（到 10-07 03:30 UTC，看夜间 dagster 批次不再丢日志）
- **验收**：10-07 03:30 UTC 之后经 Prometheus：`sum(increase(promtail_dropped_entries_total[12h]))` = 0，且 `sum(increase(promtail_mutated_entries_total{reason="line_too_long"}[12h]))` ≥ 1（超长行被截断而不是整批丢）
- **下一步**：那一行超长日志本身记为 TD-170
- **现在**：根因不是 Loki 背压：09-28..10-06 的 12 次全是 `{namespace="research", app="dagster-daemon"}` 在 22:45 / 03:00 UTC 有一行超过 256 KB，Loki 回 400，promtail 不重试、把整批 17–20 条记作 ingester_error。infra 677beb2：promtail `max_line_size: 250KB` + `max_line_size_truncate: true`（helm revision 7，5 个 pod 已加载）；`BifrostPromtailDroppingLogs` 说明补上 400 情形
- **Claim**: Release gates that prove 'nobody calls X any more' read Loki. promtail dropped 35 entries 10-02..10-05 and 18 more by 10-06 with reason ingester_error, clustered on ubt-k3s-04 (STG/DEV api pods) at ~03:00Z and ubt-k3s-02 (PROD) at 22:xxZ. loki_gate.py counts the drops and refuses to call a zero a zero.
- **Measured**: MEASURED 10-06 by paydown lane F: TD-51 gate dev 0 / stg 0 / prod 0 hits but INCONCLUSIVE (exit 3) because of 18 dropped entries; Prometheus `sum by (instance,reason)(increase(promtail_dropped_entries_total[1h]))`.
- **Evidence**:
  - `bifrost-trade-infra/scripts/release/loki_gate.py:241` — `res = prom_instant(f"sum(increase(promtail_dropped_entries_total[{secs}s]))", end)`
- **Impact**: Every compatibility-removal gate (TD-51 today, naming gates before it) needs an Owner judgement instead of a mechanical pass; real drops could hide a real caller.
- **Fix**: Find the Loki ingester limit or back-pressure hit at those times (rate / stream limits, ingester memory, the nightly batch log burst) and raise it or shape the burst; alert on promtail_dropped_entries_total > 0.
- **Ratchet**: PrometheusRule on increase(promtail_dropped_entries_total[1h]) > 0 (warning), so a gate's blind spot is visible the day it opens.
- 审批 不用批 · 代价 S · 风险 low · repos: bifrost-trade-infra

### TD-472

**P2 · S5 · 准入策略文件里的 S5 段在 Kubernetes 1.35 上 CEL 编译不过，从未装上；main 与线上不一致**

- **状态**：未开始（2026-10-31）。
- **证据**：2026-10-31 11:17 #233（owner_run_command）对 infra main f871bc4 的 `k8s/platform-rbac/40-admission.yaml` 做服务端 dry-run 被拒：S5 模板比较处 `expected type 'string' but found 'map(string, dyn)'`、`expected type 'bool' but found 'map(string, string)'` 等类型错误，来自 10-30 的 6a9bf12、f416ea7、25d7b23（`variables.s5`）。线上 `bifrost-platform-pipelinerun` 一直是 e889aa6（10-24）版；B5 改用安装快照 971956d（分支 `install/b5-admission-2026-10-31`：e889aa6 + B5 repo-url 一条规则）装上（#234）。
- **影响**：main 上的准入文件装不上去；S5 启用前必须修；在修好之前，任何人按 main 重装都会在 dry-run 失败（失败即不改，安全）。
- **修法**：把 S5 段里的列表 / map 字面量改成同构（`dyn()` 包裹或逐字段比较），每次改动在集群上做服务端 dry-run；装上后 main 与线上一致，删安装快照分支。
- **防线**：infra CI 的 statics 阶段对 `40-admission.yaml` 做与集群版本一致的 CEL 编译检查，编不过就红。
- **验收**：`git show origin/main:k8s/platform-rbac/40-admission.yaml | kubectl apply --dry-run=server -f -` 通过。
