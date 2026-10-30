# B5-h：首次 STG 发布接力 canary（方案，未执行）

范围仅为 `bifrost-platform` → `bifrost-deliver-platform` → STG。B5 当前由 **PROD platform-workers** 驱动，但目标仍是 STG。本文不授权 PROD 发布、不涉及 Trade/D10，也不要求改 DDL。

**首次无样本环境仍为 BLOCKED（R-01）**：当前代码没有面向操作者的 B5 确定名样本创建/归档/清理入口；下文列出缺口和替代步骤，不能直接照「最小演练」开门。规则写入、Owner 签批和任何人工审批统一在 **PROD 平台 `ops.bifrost.lan`**；`landing:dev` 中的 `dev` 是运行时租户，不表示应去 DEV 平台操作。本文以本次源码为依据，不声称现场入口已部署或已验证。

## 进入条件

1. 统筹在专用测试 PostgreSQL 上补跑同一提交的用例，必须设置 `AGENTRT_TEST_REQUIRED=1`，不得把 skip 当成通过。测试账户/角色按 `api/internal/agentrt/agentrttest/db.go` 配置；测试框架会创建临时数据库并应用已有迁移，不能指向业务库。

   ```sh
   export GOCACHE="$PWD/.gocache"
   # AGENTRT_TEST_PG 由统筹在环境中安全提供，不写进命令日志或报告。
   AGENTRT_TEST_REQUIRED=1 go -C platform/api test -race -p 1 -count=1 \
     ./internal/agentrt/store -run 'TestB5HCrossPG' -timeout 15m
   ```

   同时补跑原有 B5-c/f/g 的 PG 用例，尤其最终额度竞争、规则与 Close 并发、卡答复与覆盖并发。新增跨块测试不替代这些事务测试。

2. 经既有审批和发布流程部署同一审核提交；确认运行中的 workers 镜像版本。规则先保持 `stg_release: none`。核对无未知在途执行、未清理的异常租约或需要人工处置的 quarantine；保留它们的记录，不用删库“清场”。
3. 记录当前 STG 基线：可信发布的 P/U/infra、run 名/UID、三个 workload 的 UID/generation/digest、pin-sync 与 health 结果。对基线到候选 P 的整个 first-parent 区间核验授权，不能只核验 canary 的一个成员。
4. 准备一个新的低影响、可回退根的任务与候选改动，**此时先不落地**；待开门、首次启用栅栏核对完毕后，才按生产落地流程进入 main 并固定完整 40 位 P。开门前的探针 `--revision` 使用已核验且有成功 CI 的完整提交 `B5_PROBE_P`，候选落地后再核验最终 P 的精确 CI 证据。盘点所有 active mission 的历史根：首次启用可能纳入仍属 active mission 的旧根，不能仅按提交时间排除。出现额外候选时保持 none，先完成范围核对，不能删记录清场。

## 开门前只读探针

在任务目录构建（不会启动 workers）：

```sh
export GOCACHE="$PWD/.gocache"
go -C platform/api build -o "$PWD/stg-release-probe" ./cmd/stg-release-probe
```

在具有 STG 集群内部服务网络的获准运行环境执行。身份必须是部署中的实际 workers ServiceAccount；默认核对 `bifrost-platform-prod/platform-workers` 的 `serviceAccountName`，不能用管理员身份制造 PASS。二进制放置或运行环境准备如涉及集群写入，另走平台动作与既有审批。

```sh
./stg-release-probe \
  --revision "$B5_PROBE_P" \
  --sample "$B5_HELD_RUN" \
  --sample-uid "$B5_HELD_UID" \
  --workers-namespace bifrost-platform-prod
```

默认使用 in-cluster 身份；已有、经授权的 workers kubeconfig 可用 `--kubeconfig /path/to/workers-kubeconfig` 显式提供。不要输出其内容。该环境仍须能访问固定的 STG `/health` 内部地址。

`B5_HELD_RUN` 必须是一个已经存在、成功且证据完整的确定名 `stgrel-*` 样本，持有 B5 finalizer；`B5_HELD_UID` 从独立的既有创建响应/审计记录取值，不能只把本次 GET 的 UID 填回探针。探针不创建、归档或清理样本。缺样本的现场操作见下一节；不能将普通人工 run 改名、补标签/finalizer 或伪造 UID，也不能把 CREATE dry-run 通过替代真实对象上的 PATCH dry-run。

必须全项 PASS，退出码为 0，并保存输出：

| 检查 | 验收内容 |
|---|---|
| workers_identity | SelfSubjectReview 用户名精确匹配真实部署的 ServiceAccount |
| rbac | 生产适配器列出的 PipelineRun/TaskRun/Deployment/ReplicaSet/Pod/Argo/窗口权限 |
| window_rbac | create/update ConfigMap 授权；不发送窗口写操作 |
| window_read | 成功读取并解析窗口；404 明示“无窗口”，不创建；已有窗口不能被探针续租 |
| workloads | 三个 STG Deployment 均通过严格 PodDigests 与模板检查；零副本不通过 |
| argo_read | STG Application 可读，输出 UID |
| health | 固定 STG `/health` 返回有效 JSON `status=ok`，未知或失败均 FAIL |
| exact_ci | 生产读器扫描分页，精确仓库/pipeline/P，最新 CI 成功，输出 run/UID/P/U/infra |
| run_and_task_evidence | 样本 UID 与完整 clone/build/pin-sync TaskRun 证据一致 |
| finalizer_admission | 带 finalizer 的 CREATE 和有 UID/RV 守卫的移除 PATCH 均经服务器 `dryRun=All`；验证其他 finalizer 保留 |

命令只允许 GET、非持久化 SelfSubjectReview/SelfSubjectAccessReview、限定路径且明确 `dryRun=All` 的 CREATE/PATCH；拒绝 PUT/DELETE、非 dry-run 写入、exec/proxy 和重定向。对缺权限、无样本、证据未知和超时均输出 FAIL，不能降级为“基本通过”。本次开发未执行集群探针。

## 首次样本：现有入口、缺口与替代步骤

源码 `api/internal/delivery/stg_create.go` 中 `PrepareNamedDeliverRun` / `CreateNamedDeliverRun` 是内部 Go 方法；`CreatePermittedNamedDeliverRun` 由 B5 Sender 调用。它们**不是** Console action、MCP 工具或 HTTP 路由。现有 `start_pipeline_run` 使用普通 run 名称，不能提供 B5 batch/try/intent 和 hold finalizer；`plan_manifest` / `apply_manifest` 也不能替代生产构造器与 B5 身份/归档协议。当前没有可填写的“首次 B5 样本”审批动作名称，不能虚构建单步骤。

现场由统筹执行以下分支，Owner 负责后续新增能力的批准：

1. 在 PROD 平台用只读 `GET /api/v1/agentrt/rules/landing%3Adev` 确认 `stg_release: none`，查既有审计与 `cicd` PipelineRun。若已有合法 held 样本，核对独立 UID、生产构造身份、成功终态和 clone/build/pin-sync TaskRun 后，填入探针。记录样本归属及收尾负责人；探针的 PATCH dry-run 不会摘掉它的 finalizer。
2. **没有合法样本时停止自动 canary**，登记「B5 首次 held 样本入口缺失」，保留 none 与探针 FAIL 输出。替代工作是继续离线/专用 PG 验证；若只需验证候选版本能人工上 STG，可另用现有 `start_pipeline_run`（`name=bifrost-deliver-platform`、完整 `revision=P`、`params.uiRevision=U`、`params.infraRevision=infra`、明确 `who`）按既有发布窗口与 CI 流程执行。它只算人工 STG 发布验证，**不算 finalizer 验收或 B5 canary PASS**；此次文档修订不执行该发布。
3. 统筹先交付并复核一个受控的样本入口，部署后把其**真实 action/MCP/审批名称和参数**补进本节，再安排 Owner 在 PROD 平台批准一次样本操作。新增入口的设计/实施不在本修复范围内；以下是验收要求，不是当前可执行接口：复用 `PrepareNamedDeliverRun` 和确定名创建路径；由持久记录分配不与自动批次冲突的独立 batch/try/intent（不猜一个“大数字”、不手插账本）；输入固定 P/U/infra；持有且续租匹配的 `who=stgrel-bifrost-platform-<n>-<try>`、`what=bifrost-platform`、`env=stg` 窗口；丢回包按原名取证，独立保存创建/审批审计中的 UID。
4. 该入口还须有**可执行的收尾路径**：统筹保存完整原 UID 与子任务终态证据，探针验证 PASS 后，由已批准的样本执行器先按生产协议归档，再用 UID/RV 守卫仅摘 B5 finalizer，最后停止续租并释放自己的窗口；失败/未知保留原对象与审计，由 Owner 指定负责人处置。已有 B5 管理的样本则由正常观察路径先归档再摘除。没有已验证的归档和清理路径，样本准备也算未完成，不进入自动演练。样本发布若改变了 STG 内容，开门前重新记录进入条件第 3 项的基线。

本分支的替代结果是「自动 canary 待补入口」，不是放宽探针、打开规则造样本或现场拼装 PipelineRun。

## 规则操作入口（均在 PROD 平台）

当前规则接口确实存在于 `api/internal/agentrt/agentrtapi/api.go` / `rules.go`，但没有专用规则 MCP 工具或 Console 热切换按钮。由统筹通过已配置认证的 PROD HTTP 客户端调用；先 `GET /api/v1/agentrt/whoami` 核对角色、真实 actor 和 `may`，普通线程使用自己的 `X-Bifrost-Session`。不要读取或代用 Owner 的管理员令牌。

| 用途 | 请求与输入 | 生效条件 / 责任人 |
|---|---|---|
| 查当前规则 | `GET /api/v1/agentrt/rules/landing%3Adev` | 返回 `in_force`；只读 |
| 检查下一版 | `POST /api/v1/agentrt/rules/landing%3Adev/check`，JSON `{"text":"完整规则原文"}` | 只读；核对 `previous_version`、`rule_version`、`tightens`、`needs_signature` |
| 提出开门/恢复规则 | `POST /api/v1/agentrt/rules/landing%3Adev/proposals`，JSON 含 `idempotency_key`、`text`、`reason` | 统筹登记，返回 artifact/event；**proposal 不是批准，不会自动生效，也不创建 C/D 审批单** |
| Owner 签名后生效 | `POST /api/v1/agentrt/rules/landing%3Adev/sign`，JSON 含同一份 `text` 和 `signature` | Owner 在自己的签名环境对完整原文字节执行 `ssh-keygen -Y sign -n bifrost-agentrt-rule -f <Owner签名私钥路径> <规则文件>`；统筹只提交规则及返回的签名。服务端按既有 anchor 验证；以事务成功、到达 `effective_at` 且 GET 读回该版为准 |
| 免签收紧停发 | `POST /api/v1/agentrt/rules/landing%3Adev/tighten`，JSON `{"text":"完整下一版规则"}` | 已认证、有 stop 能力的既有 actor 可执行；服务端比较后才接受，无需等 Owner 签名。见下节 |

开门/恢复是 `direction: loosen`、版本严格递增，Owner 必须签完整规则、范围及有效期；签名不是问题卡答复。若现场另涉及 PROD 发布、配置或重启，则按相应动作单独在 PROD 平台审批；不能用签规则替代这些批准。

## 最小演练

1. 先满足全部进入条件、held 样本与只读探针 PASS；首次样本缺口未补齐就停在这里。统筹按上表 check/proposals，Owner 签署仅此 canary 的下一版规则，经 sign 生效并 GET 读回：`stg_release=bifrost-deliver-platform`。核对规则版本、有效期、仓库、允许路径、预期完整区间授权，以及实际 PROD workers 的启动配置和身份。不要直接改数据库、ConfigMap 或手工插记录。
2. 观察首次启用基线记录，然后按已批准路径完成上述一个新根的落地并确定 P；核对控制器接受的精确 CI 是最终 P（含最新成功 CI UID 和 P/U/infra），记录 root/op/R、批次 n、try=1、完整 S 和窗口 who/gen。不要拿开门前 `B5_PROBE_P` 的 CI 替代最终 P；已完成 finalizer 验收并按协议清理的样本无需再次持有。存在其他待发布成员或输入变化就执行下文冻结步骤，重新评估范围。
3. 验收一个 `started`、一个预算扣减、一个确定 run 名和 UID、一个 `bound`；GET/409/回包丢失不能产生第二 UID。正常演练不主动制造失败、不删 run、不干预租约；这些故障由离线与 PG 测试覆盖。
4. 必须先有原 run 的终态 `run_evidence`，再摘 finalizer，再有 `verify` 和成员结算。检查三个 workload 摘要、实际 P/U/infra、pin-sync、health；外部 covers 不能跳过原 run 的终态与预算决策。成功应为 `verified`、一个 `member{delivered}`、一个 `settled`，批次 CLOSED。
5. 连续观察至少三次 reconcile，确认同一 run/UID、预算与记录数稳定，没有第二次 Create。真实失败仍遵守三次总预算与终态起算的 10/30 分钟退避；不为让 canary 变绿主动追加预算。15 分钟 rollout/health 观察期跨重启不能归零；未知读取不能冒充确定失败。
6. 保存去敏验收证据，按下文免签 tighten 恢复 `stg_release: none`，完成停发核对。是否长期启用另由 Owner 签下一版规则决定。

## 停止与回退

### 默认停发：免签收紧到 none

这是本方案现有、无需重启 workers 的停发路径。统筹用自己的已注册 actor 在 PROD 平台执行；**免签 tighten 不需要 Owner 先点头**，执行后向 Owner 报规则版本、时间、在途清单和异常。Owner 的动作是决定后续处置、人工回退或签署恢复规则。若组织流程要求签名，也可让 Owner 签同一份 tighten 原文后走 `/sign`；等待签名不应冒充已经停发。

1. 保存 `GET .../rules/landing%3Adev` 和 `GET .../events?stream=landing%3Abifrost-platform`（每页最多 500，按最大 `seq` 用 `after_seq` 翻页）。记录所有已提交许可的 `deadline`、started/permit/bound、UID、窗口 who/gen/RV。GET 的 `in_force=null`、请求失败或存在无法解释的规则版本时，不宣称完成冻结，交 Owner 核对最新规则；过期 non-none 仍可能允许观察，不能把无有效规则等同已写 none。
2. 从当前完整规则生成下一版：仅版本 +1、`direction=tighten`，把 `bifrost-platform` 条目的 `stg_release` 改为 `none`，保留其他仓库、allow/deny、额度、`effective_at` 和 `expires_at`。沿用当前已生效的时间使它在事务提交后即可被选择；**不延长有效期、不提前于上一版生效时间**。先 `/check`，必须 `tightens=true`、`needs_signature=false`、`previous_version` 等于刚核对的版本。存在未来版本或并发修改时会拒绝，读取并重新核对最新完整规则后重做，不盲目重放；无法证明纯收紧就交 Owner 签署。

   以下是操作者在获准主机上的实际 HTTP 调用格式（本次未执行）。`B5_HTTP_CONFIG` 是操作者已配置的 PROD 普通 operator 认证文件，含自己的会话头；不打印、不写入报告或仓库。所有命令失败即停下核对。

   ```sh
   (
   set -eu
   B5_RULE_URL='https://ops.bifrost.lan/api/v1/agentrt/rules/landing%3Adev'
   curl --fail --silent --show-error --config "$B5_HTTP_CONFIG" \
     "$B5_RULE_URL" -o b5-rule-before.json
   python3 - <<'PY'
   import json
   from pathlib import Path
   rule = json.loads(Path('b5-rule-before.json').read_text())['in_force']
   assert rule and rule['rule_id'] == 'landing:dev'
   rows = [r for r in rule['repos'] if r['repo'] == 'bifrost-platform']
   assert len(rows) == 1
   rows[0]['stg_release'] = 'none'
   rule['rule_version'] += 1
   rule['direction'] = 'tighten'
   text = json.dumps(rule, ensure_ascii=False, indent=2) + '\n'
   Path('b5-none-rule.json').write_text(text)
   Path('b5-none-request.json').write_text(json.dumps({'text': text}))
   PY
   curl --fail --silent --show-error --config "$B5_HTTP_CONFIG" \
     -H 'Content-Type: application/json' --data-binary @b5-none-request.json \
     "$B5_RULE_URL/check" -o b5-none-check.json
   python3 - <<'PY'
   import json
   from pathlib import Path
   old = json.loads(Path('b5-rule-before.json').read_text())['in_force']
   check = json.loads(Path('b5-none-check.json').read_text())
   assert check['tightens'] and not check['needs_signature']
   assert check['previous_version'] == old['rule_version']
   assert check['rule_version'] == old['rule_version'] + 1
   PY
   # 仅在前述核对成功后执行；这是生效操作，不是 check/proposal。
   curl --fail --silent --show-error --config "$B5_HTTP_CONFIG" \
     -H 'Content-Type: application/json' --data-binary @b5-none-request.json \
     "$B5_RULE_URL/tighten" -o b5-none-applied.json
   curl --fail --silent --show-error --config "$B5_HTTP_CONFIG" \
     "$B5_RULE_URL" -o b5-rule-after.json
   )
   ```

3. 核对 tighten 响应的 `rule_version`、`event_id`、`signed=false`、生效时间，以及后一次 GET 的 `in_force` 等于提交的规则。保存 `rule:landing:dev` 流中的 `rule_tightened` 事件。只得到 HTTP 200、check 或 proposal 结果不算停发。
4. **生效时点与在途边界**：规则事务提交并生效后，后续最终授权事务会读到 none 并拒绝新许可。workers supervisor 每 15 秒检查一次规则；下一次成功检查后关闭 B5 controller，等待续租线程结束并尝试只过期自己的窗口。15 秒是调度间隔，不是已停的硬上限，前一轮 I/O、事务锁和窗口清理可能尚未完成。已提交许可的发送检查不直接重读规则，原许可最长 2 分钟且受租约代际约束；它和已经开始的 POST 都不能被这次规则写入追溯取消。以实际许可 deadline、窗口与原 UID 核对，不能在 tighten 返回时承诺零在途。
5. **确认停了**：读回 none 后，跨过所有已知许可 deadline，并连续观察至少三次检查周期（间隔至少 15 秒）：没有新的 B5 started/permit、新确定名或新 UID；B5 记录与卡片不再增长，自己的窗口不再续租且已释放/过期，别人的窗口不动。可用只读 `kubectl -n cicd get pipelineruns -o json`、`kubectl -n cicd get configmap bifrost-release-window -o json`、上述 events API 和 PROD workers 日志交叉核对。只统计 `note.stg_release`/B5 卡事件，普通 landing 仍可能写同一个流。有旧许可、未知 POST 或延迟出现的 UID 时继续按原身份人工跟踪，记录「规则已收紧，在途未清」，不能宣称全部停止。

none 的兼容语义是**整个 B5 冻结**：不继续自动取证、归档、核验、结算、卡片或摘 finalizer；统筹在外部保存原 run/TaskRun 的只读证据，待批准恢复后由正常路径消费。它不取消 Tekton、不回滚 workload，也不关闭普通 landing。

### 需要保留自动观察时：另行批准的 workers 配置变更

当前没有热 live-off API。`AGENTRT_LANDING_LIVE_REPOS` 只在 workers 启动时读取：变量存在时（包括显式空字符串）优先于旧 `AGENTRT_LANDING_LIVE`；只有新变量不存在时，旧值 `on` 才启用 platform。共享 `c.Live` 由启动接线设置，保留另一个 live 仓库可能仍为 true，因此仅移除 platform 或仅把旧变量设为 off 都不足以证明 B5 已停。

若 Owner 明确选择「保留非 none 规则、只停新发送并继续观察」，须另外批准 **PROD workers 配置及滚动更新**：将生效的 `AGENTRT_LANDING_LIVE_REPOS` 显式设为空，并把旧变量设 off 以消除回退歧义。统筹先准备 infra 的实际配置提交和差异，Owner 批准该提交及 PROD 影响后才按发布窗口用 `push-main.sh` 推送；PROD Argo 自动同步，不能等推送后再补批准。需要人工同步或重启时，在 PROD 用 `request_action` 为相应 C 级动作建单（附 `reason`、`rollback`、`work_id=W-31`），由 Owner 在 PROD Console 批准，按 `get_request` / `wait_for_request` 核对执行结果：

| action | params |
|---|---|
| `gitops_sync_app` | `{"name":"bifrost-platform-prod"}`；先确认 HEAD 就是已批准的 infra 提交 |
| `rollout_restart_deployment` | `{"namespace":"bifrost-platform-prod","kind":"Deployment","name":"platform-workers"}` |

不能只改本地 shell 环境或只重启而不改源配置。统筹核对所有新 Pod 的配置来源、UID/镜像/启动日志及旧 Pod 已退出，再按上面的许可/UID/窗口条件验收停发。

这会停止**所有原 live 仓库**的新 landing claims/push，已有落地的恢复仍继续；必须把受影响仓库清单写入该 PROD 变更的批准范围。尚无已批准配置提交/动作时，使用前面的 none 路径，并如实接受自动观察也会停止。恢复 live 配置须 Owner 另批对应 PROD 变更并核对全部新 Pod；none → non-none 还须按规则表提出并由 Owner 签下一版 loosen。恢复前先核对在途原 UID、样本清理、完整 P 授权和所有候选，不能自动沿用原 canary 批准。

### 在途处置与内容回退

- 记录所有已扣预算、started/permit、bound UID、归档证据和窗口版本。Running、读取未知、丢租或未绑定过期 404 均不能视为“没发出去”；后者保持 `unknown_execution` quarantine，不补建、不退款、不释放执行槽。
- 不强摘 finalizer、不删除 PipelineRun/事件、不手动改 generation、不覆盖别人的窗口、不用问题卡答复代替发布审批。原 UID 晚到时仍按原身份取证。
- 若 STG 内容需回退，由 Owner 选择已核验的前一组 P/U/infra 与摘要，通过平台人工发布流程审批并执行；先确认原 run 的真实状态、没有争抢窗口。保留新人工 run 的 UID 和部署证据，待批准恢复非 none 后由正常观察路径消费。自动接力不得自行向后部署。
- 未达到以上成功条件即记录 canary FAIL/未完成，保持自动发布关闭；不因此触发 PROD 发布。

代码核对点（相对 `platform/api/internal/`）：`agentrt/agentrtapi/{api,rules}.go`（规则路由）、`agentrt/store/rules.go`（版本、事务及签名）、`agentrt/rules/{tighten,landing}.go`（收紧比较）、`server/agentrt_stg_release.go`（15 秒 supervisor）、`agentrt/landing/repos.go` / `server/agentrt_landing.go`（启动 live 配置）、`agentrt/store/stg_gate.go` / `agentrt/stgrelease/send.go`（许可 deadline 和已提交发送）、`delivery/stg_create.go`（样本构造与创建）、`actions/catalog.go`（实际动作及参数）。

本文件仅是待批准的操作方案，本次没有打开规则、运行 canary 或写集群。
