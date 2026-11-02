## 现有防线

| 防线 | 位置 | 挡什么 | 强度 | 范围与缺口 |
|---|---|---|---|---|
| Dagster Pod 有回收孤儿的 PID 1，run 排队有上限（TD-473） | research `tests/orchestration/test_daemon_manifest.py`：`test_dagster_pods_have_a_reaping_pid_1`（daemon 与 webserver 都 `shareProcessNamespace: true`）、`test_runs_queue_behind_a_concurrency_cap`（instance 用 `dagster._core.run_coordinator` 的 `QueuedRunCoordinator`，`max_concurrent_runs` 1–3） | daemon 当 PID 1 不回收 code server 每分钟留下的孤儿，约 20 天打满 pids 上限、所有 schedule 停摆（2026-10-30，61 小时）；恢复时所有漏掉的 tick 同时起 run，撑爆 4Gi | blocking（ci-python） | 只守清单；运行时没有 pids 占比告警（提议未做），心跳告警是唯一的运行时信号 |
| 预算用完不困住任务（TD-384） | bifrost-platform `api/internal/agentrt/store/missionbudget_test.go`：`TestRetryOnSpentBudgetLeases`（再来一轮在一轮里就被领走）、`TestSpentBudgetBlocksReadyTask`、`TestSpentHoursBlockReadyTask`（预算用完的 ready 任务挂起并出写明预算的卡） | 预算用完后任务悄悄停在 ready、Owner 选了再来一轮也派不出去 | blocking（ci-platform） | 只管次数和小时两种预算 |
| 按确切提交发布：每个仓库按自己要拉的提交核对（TD-295） | platform `api/internal/delivery/`：`TestPinnedPlatformDelivery`、`TestPlatformUnknownDoesNotMaskMissing`、`TestPlatformSameSHARefused`、`TestPlatformPollingErrorRequiresSeenPresent`、`TestPlatformHexadecimalNames`、`TestPlatformCloneRefParamsFollowInfra`（表里的参数等于 infra 流水线定义里拉代码任务用的参数）、`TestPlatformCreatedRunsUnchangedFromMain`（今天在用的四种写法建出的整个对象和 `26a10b5` 逐字节相同）、`TestNonPlatformRefChecksMatchMain`（别的流水线对镜像的请求和结果和主线逐条相同） | 发布单写的提交就是发的提交；镜像里没见到的提交不会被拿去起发布；这项能力不悄悄改变别的流水线 |
| FE eslint | `ci-frontend：`npm run lint // { echo "WARN: lint found issues (non-blocking for now)" }`` | lint 类回退 | warning | MEASURED 10-07 frontend `1d471e48`：**0 error / 68 warning**（大多数是 `react-refresh/only-export-components`），「改成阻塞没有成本」仍然成立 |

## 各类债现在挡没挡住

| 债的类别 | 挡住了吗 | 靠什么 | 缺口怎么补 |

## 待建防线（第 2 轮提议，按覆盖面排序）

### CI gates release (one switch that makes every test ratchet real)
