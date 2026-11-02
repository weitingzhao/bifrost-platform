## 条目

### TD-85

**P1 · trade (round 1) · One database password reaches everything: any DEV pod can write PROD Trade and all of Golden Source**

- **状态**：观察中（到 10-09 01:00 UTC，看撤权后 24 小时内 Postgres 日志里 `analytics_writer` / `market_reader` / `role_matrix_reader` 的 `permission denied` 为 0——夜里的 Dagster 批次与 Research 写入会用到 analytics_writer；为 0 就进「待你签收」）。防线已全部落地（10-08）：期望矩阵 + 只读检查 `k8s/data/role-matrix/`（infra 5ff1624、99e557e）接进 `release-check.sh <env> before`；每日 CronJob `data/db-role-matrix`（05:15 UTC，登录 `role_matrix_reader`，只 CONNECT，Secret `data/db-role-matrix-reader`）与告警 `BifrostDbPrivilegeDrift` 已 apply；手动 Job `db-role-matrix-manual-1008b` → `role-matrix: 0 difference(s)`。两个 db-step（`2026-10-07-td85-analytics-writer-create`、`2026-10-07-td85-role-matrix-reader`）已执行并记 done

### TD-95

**P2 · research-control · No release path is gated on CI: deliver-research ships SHAs whose CI is red (CI starts 7s after deliver), and release.sh never checks CI for Trade**

- **状态**：观察中（validate-git-sha 与 research-lint-test 已 apply 进 deliver-research / build-research-dagster；下一次 research 发版首跑）

### TD-448

**P1 · 交付 · STG 平台发布用的 Dockerfile 是 cicd 里的副本，改仓库文件不生效**

- **状态**：**2026-10-28 夜班**：infra 分支 `w31/td448-deliver-dockerfile` `baef4c8`（Codex 649 写、统筹复核）：STG 管线新增 `clone-infra`，stage 从本次克隆拷 Dockerfile；PROD 管线只提升 STG digest、不构建，未改；静态测试 `make check-platform-deliver-dockerfiles`（旧管线 6 项红、新 5 项绿）。待 Owner 推 infra main 并用管理员 kubeconfig `kubectl apply -f k8s/cicd/tekton/pipeline-deliver-platform.yaml`，再按验收跑一次 STG 发布核对日志。之前：未开始（2026-10-27 登记；当天由 Owner 手工刷新 `bifrost-platform-api-stg-dockerfile` 解了 S2 的燃眉之急，五份 STG Dockerfile 副本当时与 infra `d32eff8` 逐字一致）。

### TD-451

**P3 · Agent 工具 · `bifrost-trade-api` MCP 用 HTTP 打网关，被重定向后全部 404**

- **状态**：待 Owner 改配置（2026-10-27 晚）。实测：Node 的 fetch 会丢掉手设的 `Host` 头，所以请求实际打到裸 IP（404）；把 `TRADE_API_GATEWAY` 写成 `https://stg.trader.bifrost.lan`、删掉 `TRADE_API_GATEWAY_HOST` 后，客户端 `tradeGet` 四个健康端点都 200（Node 认这张证书，不用跳过校验）。Agent 改自己的 MCP 配置被 auto mode 拦下，改由 Owner 跑一条命令。
