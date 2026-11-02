## 条目

### TD-183
**P3 · market-data · W3 snapshot archives are verified readable, but typed restore and first option_daily / short_volume archives remain unverified**
- **Evidence**:
  - `/stocks/REQUEST-w3-archive-before-delete-2026-10-05.md:20` — `option_snapshot 盘中行 | 2026-09-08 | 约 10-08 02:15 UTC（30 天窗口）`
  - `/stocks/REQUEST-w3-archive-before-delete-2026-10-05.md:21` — `option_daily | 2024-10-01 | 11-01 02:15 UTC`
- 审批 不用批 · 代价 S · 风险 med · repos: bifrost-platform-plugin-market-data

### TD-467
**P2 · S5 · 检查器镜像地址用的是集群内服务名，节点拉不到**
- **证据**：2026-10-31 00:3x ci-infra 首次阳性运行 `bifrost-ci-infra-1793424335` 在 resolve 步骤 `ErrImagePull`：Task 镜像写成 `registry.cicd.svc.cluster.local:5000/…`，节点上的 containerd 解析不了集群内服务名（平台自己的镜像都用 `192.168.10.73:30500/…`）。ci-infra 已改（本提交）；S5 同样写法未改：platform `api/internal/agentrt/landingci/s5.go:51`（`s5ImageRepository`）、`landingci/s5trusted/contract.json:2,363`，以及 infra `k8s/cicd/tekton` 下 S5 的 Task 与 `s5-run-template.json`、VAP 字面量。
