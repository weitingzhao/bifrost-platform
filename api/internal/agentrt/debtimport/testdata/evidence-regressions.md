## 条目

### TD-441
**P1 · 人工审批归属 · PROD 审批与目标环境执行之间缺少通道**
- **证据**：platform 基线 `45ad51ee` 的 `api/internal/actions/catalog.go:452` 指示到目标平台建单；`:466` 的 `void_exam_paper` 也有同类说明；`api/internal/server/agentrt_nomerge.go:32`、`agentrt_voidpaper.go:28` 只允许本机 runtime 并引导去另一个平台；`api/internal/approvals/service.go:200` 在建单前没有环境防线。PROD 没有 STG runtime DB 的执行能力，改一句入口文案不能补齐执行链。

### TD-130
**P1 · ops-control · ops-autopilot acts on the shared cluster's data layer from the Owner's laptop (local bdev platform-api, role all); the in-cluster STG/PROD autopilots idle on an empty checklist, and each of the three keeps its own throttle**
- 审批 要你批 · 代价 M · 风险 med · repos: bifrost-platform

### TD-266
**P3 · flex-ib · The flex worker's system messages never leave the pod: it publishes to 127.0.0.1:6379, which is refused in-cluster, so the UI toast for a cash-ingest run is dropped with only a WARNING**
- 审批 不用批 · 代价 S · 风险 low · repos: bifrost-platform-plugin-flex-query

### TD-288
**P2 · infra · ubt-k3s-01, the sole control plane and etcd member, went down at 2026-10-10 01:18Z without a shutdown, and none of its boots since June recorded a clean shutdown; nothing alerts on an unplanned node reboot**
- 审批 不用批（查硬件要你到场）· 代价 S · 风险 med · repos: bifrost-trade-infra

### TD-443
**P1 · NAS 监控 · smartctl_exporter 0.14.0 对 NVMe 整页 500，NAS 四块盘的 SMART 都看不见**
- **证据**：`up{job="smartctl",host="nas"}` 自 2026-10-24 17:07:00 CDT 起为 0，Prometheus 报 `server returned HTTP status 500`；直接读 `http://192.168.10.20:9633/metrics` 的正文：`collected metric smartctl_device_bytes_written label:{name:"device" value:"nvme0"} ... with unregistered descriptor`。pool2 建于 17:07:27（设备名时间戳 `1792879647`），SSD 有了第一笔写入后触发。`BifrostNasSmartBlind`（critical）与 `BifrostHostSmartExporterDown` 自 10-24 22:08Z 起在报。上游 v0.15.0 的 #329（issue #326）修的就是这条。镜像：`scripts/host-exporters/nas/compose.yaml:58`；版本：`scripts/host-exporters/versions.env:21`。细节：`agent-config/work/multi-agent/CUT-w68-nas-tiering-2026-10-26.md` 第 3 节。
- 审批：推 infra main 由 Owner 推（冻结期）；NAS 上一步由 Owner 跑。
