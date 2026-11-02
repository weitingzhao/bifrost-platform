## 条目

### TD-218

**P2 · data · Every backup copy (Barman base+WAL, logical dumps hot and cold, the W3 archive) is on the one NAS 192.168.10.20:/volume1, and the open offsite decision is not in the ledger**

- **状态**：未开始（Owner 10-07 暂缓）
- 审批 PROD 变更（要你批） · 代价 M · 风险 low · repos: bifrost-trade-infra

### TD-434

**P1 · rbac · cicd · both platform service accounts can rewrite any ConfigMap in `cicd`, including the script a job runs every 15 minutes with the Gitea admin token**

- **状态**：在做——已应用，差验收（2026-11-01 只读核对）：`bifrost-cicd-configmap-writers` 集群里 2026-10-24T14:30:43Z 建、绑定 `Deny`，规则与 main `k8s/platform-rbac/46-cicd-configmaps.yaml` 一致（差别只在服务端默认字段）。验收要冒充两个平台 SA 做服务端 dry-run，只有 Owner 身份能跑（`python3 scripts/check_admission_guards.py --live`，看 `*-writes-github-sync-scripts` 各例被拒）；Agent 身份跑会跳过（退出 3），本次没跑。之前：在做（2026-10-24 统筹线程记；TD-432 切法的子代理发现，统筹线程读清单核实；与 TD-432 一起修，4.1 自动落地开工前必须先修。准入策略 `k8s/platform-rbac/46-cicd-configmaps.yaml` 已写，等 Owner 服务器端 dry-run 后 apply，见 TD-432 操作单第 2b 节）
- 审批 要（准入策略与 RBAC 归 Owner）· 代价 S · 风险 medium · repos: bifrost-trade-infra, bifrost-platform

### TD-440

**P1 · 本机 Git 身份 · 管理员凭证缓存与历史推送身份待逐事件核定**

- **状态**：在做（2026-10-25，Work 565；Owner 已清缓存，新 metadata 检查已准备，历史血缘追溯和 Owner 验收未完成）。
- 审批：不推送、不改缓存的 metadata 检查不用；凭证变更/真实写探针由 Owner 操作。历史修正仅记录证据，不改写 Git 历史。

### TD-443

**P1 · NAS 监控 · smartctl_exporter 0.14.0 对 NVMe 整页 500，NAS 四块盘的 SMART 都看不见**

- **状态**：在做（2026-10-26；修复已提交到 infra 分支 `w68/nas-tiering`，等 Owner 推 main 后在 NAS 重跑一步）。
- 审批：推 infra main 由 Owner 推（冻结期）；NAS 上一步由 Owner 跑。

### TD-444

**P2 · NAS 存储 · 本机 Time Machine 每小时让 RAID5 饱和约 15 分钟，集群写延迟随之到 1.2 秒；分层方案待定**

- **状态**：未开始（2026-10-26 设计已出，等 Owner 选 A / B / C）。
- 审批：要你定 A / B / C 和归档副本方式（CUT 第 9 节）；NAS 与本机设置都是 Owner 做；B / C 的集群清单改动按 C 级单。

### TD-473

**P1 · research-control · dagster-daemon 自己当 PID 1、不回收孤儿进程：连续运行约 20 天就攒满 pids 上限，整个 Dagster 停摆；恢复时 38 个漏掉的 schedule 会同时补跑**

- **状态**：在做（2026-11-01）。修复在 research `badd5d6`（ci-python 绿，分支 `agent/td-473/a1`）；上线走 #255 `owner_run_command`（D 级，你在 Console 批后本机 `owner-run.sh`：推 research main → 活跃 run 为 0 → 服务端 dry-run → server-side apply 接管字段）。`apply_manifest` 这次走不通：C 级只收 main 头上的提交，而且这几个对象一直是手工 client-side apply（managedFields：ConfigMap 09-09、Deployment 10-07），流水线的 server-side diff 撞冲突（`plan-badd5d69-1793575648`：`conflict with "kubectl-client-side-apply" using v1: .data.dagster.yaml`）。#255 用 `bifrost-applier` 接管后，research-release skill 写的「改 dagster.yaml 用 apply_manifest」才成立。
- 审批：apply_manifest C 级（你在 Console 批）· 代价 S · 风险 low · repos: bifrost-research
