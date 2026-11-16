# 沟通层 第 0 步：交规（定稿草案 v1，8.2，2026-11-16）

依据：`brain/comm-nextgen-design-v1-2026-11-16.md`（含 1.1、1.2、落地参数、Owner 的决定）、`brain/comm-nextgen-disposition-2026-11-16.md`（38 个主题、52 处没说清的）。
这一页是规范，写的是「必须怎么做」；理由只写一句。编号 §n 供代码、登记表、CI 校验引用。

## §1 两种车道
| | 事件道 | 指令道 |
|---|---|---|
| 说的是 | 「某事变了」 | 「请你做某事」 |
| 接收方 | 任意多个订阅者，发布方不知道是谁 | 恰好一个，有名有姓 |
| 带什么 | 指针，不带内容 | 参数、期限、幂等凭据，要结果 |
| 权限 | 只有平台（账到总线的中继）能发；谁能订由登记表定 | 每条指令按动作目录判级别（和现在的动作、卡同一套） |
| 现在 | 通车 | **只定规矩不通车**；预留主题前缀 `cmd.>`，所有身份默认拒绝 |
规则：
- E1 命令和锁不是事件。窗口、冻结、卡的批准仍由各自的状态和动作管；总线只宣布「它变了」。
- E2 事件只是提示，状态到源头读（事件里的 `ref`）。丢一条、晚一条、重一条都不能让读的人做错事。
- E3 任何人要宣布变化，都先经平台写账；平台查权限；中继把账推上总线。Agent、流水线、脚本对总线**只读**。
- E4 交易指令（D10）不接。将来接也只能走指令道，靠登记、Owner 签字接入。

## §2 信封（CloudEvents 1.0，结构化 JSON 模式）
必填（标准属性）：`specversion`=`"1.0"`；`id`（ULID，全局唯一，也是去重键）；`source`（发布方，URI 形式：`bifrost://<tenant>/<env>/<门或服务>`，如 `bifrost://rocket/stg/gate-git`）；`type`（主题，见 §3，如 `git.main.advanced`）；`time`（RFC 3339，变化发生的时间，不是推送时间）；`subject`（关于谁：仓库+提交、卡号、席位号，自由文本但按主题定格式，见登记表）。
扩展属性（小写字母和数字，CloudEvents 规定）：
| 名 | 必填 | 意思 |
|---|---|---|
| `tenant` | 是 | 租户：`rocket`（平台本身）、将来 `trade`、`finance`、`plugin-*` |
| `env` | 是 | `prod`、`stg`、`dev`（本机） |
| `cursor` | 是 | 这条事件在**本环境账里**的位置：`<事务号>.<事件号>`（`eventlog` 现成的游标，按提交顺序，读快照下界保证不跳号）。订阅者的游标以它为准 |
| `ref` | 是 | 去哪读状态：平台读口的相对路径，如 `/api/v1/delivery/context?repo=bifrost-platform` |
| `actor` | 是 | 引起变化的是谁：`seat:<席位号>`、`service:<名>`、`owner` |
| `causeid` | 否 | 因哪条事件而起（那条的 `id`）；拼成一件事的全程 |
| `traceparent` | 否 | W3C Trace Context，跨服务追踪；有就带 |
| `change` | 否 | 一个主题内的细分（如窗口的 `held`/`released`/`expired`/`queued`） |
| `key` | 按主题 | 「按键有序」主题的键（仓库、卡号、席位号） |
不许有 `data`（内容）。整条信封 ≤ 2 KiB，超了拒写。提交号、卡号这类只放 `subject`/`key`，**不进 NATS 主题名**（无限多的主题名吃内存，盘点第 4 节第 8 条）。

## §3 主题
- 事件类型 `type` 三段：`领域.对象.变化`，小写，`[a-z0-9]+`，封闭词表（§4）。
- NATS 上的主题名：`ev.<tenant>.<env>.<type>`，如 `ev.rocket.stg.git.main.advanced`。租户和环境是前缀，不算在「三段」里（解第 4 节第 24 条：信封的三段式和租户段不冲突）。
- 粒度：按「对象.变化」细切（38 个），**不**合成 `对象.changed`：NATS 通配符让细粒度不花钱，订阅者用 `ev.rocket.*.approval.card.*` 一次订一类（解第 27 条）。
- 新主题先进登记表（§5）才能发；CI 拒绝代码里出现未登记的 `type`。

## §4 词表（定稿 v1：38 个，事件道）
沿用去向表第 3 节的 38 个，改三处：
1. `decision.entry.recorded`、`owner.attention.changed` 暂不开（没有门，共享笔记不是源头）。等它们有了平台里的写入口再登记（解第 19 条）。→ 现开 36 个。
2. `release.window.changed` 只宣布窗口本身；「轮到你」发 `seat.mail.arrived`（收件人是排队的席位）。
3. `seat.liveness.changed` 的发布方是「判死器」（§9），不是席位自己。
保证等级三档（登记表 `guarantee` 栏）：`best-effort`（丢了靠对账，订阅方可离线丢）；`durable`（订阅方离线期间攒着）；`ordered`（durable 且同一 `key` 保序）。
（36 个主题的逐条登记见 `api/internal/bus/registry/rocket.yaml`。）

## §5 登记表（主题的户口，进 Git）
格式：AsyncAPI 3.0 的 YAML，每个租户一个文件（`registry/<tenant>.yaml`），每个主题一个 channel，加 `x-bifrost` 扩展：
```yaml
channels:
  git.main.advanced:
    address: ev.rocket.{env}.git.main.advanced
    x-bifrost:
      tenant: rocket
      version: 1            # 信封里 subject/key 的格式版本；不兼容改动 +1，旧版并存一个发布周期
      lane: event
      guarantee: ordered
      key: repo
      subject: "<repo>@<40位提交>"
      publisher: gate-git   # 唯一发布方（中继代发，权限按它判）
      ref: /api/v1/delivery/context?repo={repo}
      reconcile: "gate-git 每 5 分钟比对 Gitea branches/main"   # 丢了靠什么对上；没有就写 none 并说明
      on_unread: stale      # 读不到状态时往哪失败：closed（按最坏）/ open（按放行）/ stale（标陈旧，不判）
      retention: 30d        # 总线上留多久；账永久（§7）
      consumers: [platform-workers, console, seat:*]   # 谁能订（生成 NATS 权限）
      exports: []           # 给哪些别的租户（租户间契约，两边都要写）
      replaces: [L04, L06, X-A28]   # 顶掉的旧通道（去向表编号）
```
校验（CI，`registry/check`）：字段齐全；`type` 三段且在词表；`publisher` 唯一；`consumers` 和 `exports` 两边对得上；`on_unread` 有值（解第 5 条）；`reconcile: none` 必须带一句理由（解第 39 条）；代码里 `bus.Publish("<type>")` 出现的每个 type 都已登记。
NATS 权限只从登记表生成一份（中继和身份服务读同一份，解第 30 条）。

## §6 席位记录
一条记录一个席位，放平台库（不放 statefile）。字段：
`seat`（席位号，如 `8.2`）；`tenant`；`title`；`bindings[]`（`{kind: claude-desktop|claude-cli|codex|local-model, session, since}`，一个席位可同时绑多个）；`doing`、`blocked_on`、`next`（各一句）；`ledger_cursor`（写这条记录时读到账的哪一条）；`updated_at`；`updated_by`（哪个绑定写的）。
- 席位自己写；变了才发 `seat.status.changed`。心跳不上总线、不进记录，只更新 `last_seen`（解第 18 条）。
- 地址写席位，不写会话：`seat.mail.arrived` 的 `key` 是席位号；绑定在记录里（解第 51 条：会话号和线程号都是绑定的一种，尾注照旧带会话号，另加 `Bifrost-Seat`）。

## §7 账、游标、中继
- **账就是现有的 `agentrt/eventlog`**（底座 0.2 落点说明，duty/from-biz-2026-11-16-comm-layer-placement.md）：不建第 5 本账。总线主题是 `eventlog` 的事件类型，由 `bus/registry` 声明；`subject`、`ref`、`key`、`change` 放在事件 body 的固定字段里，`tenant`、`env` 由平台配置给出——不加列。
- 每加一个主题：登记表一行 + Go 词表（由登记表生成、测试对齐）+ 库里 `event_type_closed` 约束一份迁移（加类型，不毁数据，不用卡）。登记表是唯一来源。
- 账永久；只记领域事件。心跳、送达回执不进账（回执另放，留 7 天，解第 16、40 条）。
- **游标以账为准**（解第 23 条）：用 `eventlog` 的 `<txid>.<event_id>` 游标（`store/eventcursor.go`，搬进 `eventlog` 共用，搬之前先问 Codex）。订阅者自己存游标；发现漏了就到平台口子 `GET /api/v1/bus/events?after=<cursor>` 补读。NATS 的消费者位置只是加速，重建总线后作废也不错。
- **中继**（`bus/relay`，workers 里，单例）：按游标读账里登记过的类型 → 发 NATS（`Nats-Msg-Id`=事件 id）→ 收到 PubAck 后才把游标写库。重启从库里的游标接着发；重复由订阅者按 id 去重（解第 11、14 条）。每 5 分钟对账：账的游标和流里最后一条的游标比，对不上就从缺口重发并发 `reconcile.drift.found`。中继和第一个真订阅者一起上（旧 relay 先铺路没车被删的教训）。
- 哈希链按 stream 串行：总线事件的 stream 按主题的 key 取（如 `git:<repo>`），同一 key 才串行（解第 15 条）。
- 状态和事件同一个事务写（`eventlog.InTx` + `Append`）；四样状态搬进库之前由门「事后宣布加对账」（解第 1、2 条）。

## §8 五个入户口子
1. 服务（Go）：`bus.Subscribe(type, fromCursor)`；常驻服务才建 NATS 持久消费者（解第 9 条）。
2. 浏览器：Console 连平台的 SSE 口子（`/api/v1/bus/stream?types=…&after=<cursor>`），**不直连 NATS**（解第 25 条）；平台一条连接扇出。
3. 脚本和流水线：一个小命令 `bifrost-bus wait <type> --match <subject> --after <cursor> --timeout <s>`，经平台口子长轮询（不各占消费者）。
4. Agent：MCP 工具 `wait_for_event`（长轮询）和现有等信命令改用同一个口子；没有会话在线时由调度器起一次性会话（§9）。Codex 用同一个小命令（3）。
5. 手机：通知器只订 `durable` 的几类，「发一次记已发」存平台库（幂等键=事件 `id`+通道，宁多勿少，解第 22 条）。总线挂了的报警不走总线（§10）。

## §9 判死器、调度器
- 判死器：workers 里一个定时器，按席位记录的 `last_seen` 判沉默，发 `seat.liveness.changed`（解第 17 条）。
- 调度器：席位有 `durable` 的信、且没有绑定在线时，起一次性会话。它要的 auto mode 放行规则只备稿，Owner 应用（Owner 11-16 已定方向）。

## §10 不依赖总线的路（火箭自救）
- R1 火箭自己的发布路径（deliver-platform*、窗口、冻结）**不依赖总线和平台读口**：流水线照旧直读集群里的窗口和冻结；读不到按冻结；破窗走执行卡（解第 4 条，主统筹补法 2）。
- R2 总线挂了的报警走 Prometheus → Alertmanager 的独立路（不经平台、不经总线）。
- R3 门禁依赖的（CI 绿没绿、窗口、冻结）新旧并排对账后再切。

## §11 NATS 落地（第 1 步照此装）
单节点 R1，`nats:2.15.3`，放在已有的 `data` 命名空间（`nats.data.svc:4222`；执行卡按准入规则不能建命名空间，2026-11-16 卡 #581；清单在 infra `k8s/bus/`，只对两个平台命名空间和 monitoring 开端口）；数据放 `local-path`（节点本地盘，不放 NFS）；`sync_interval: always`；JetStream 限额显式写（内存 256Mi、文件 10Gi）；容器内存上限 1Gi；每个租户一个账户（现在只 `ROCKET`，结构按多个写；系统账户单列）。
流：每个租户每个环境一个流 `EV_<TENANT>_<ENV>`，主题 `ev.<tenant>.<env>.>`，文件存储，`max_age` 30 天，`max_bytes` 2Gi（回执流 512Mi；两个环境合计 5Gi，在账户 10GB 之内），`discard: old`，`duplicate_window` 2 分钟（解第 42 条）。
身份：第 1 步先用两组静态用户（`relay`，workers 用：发 `ev.rocket.>` 和回执、建流、建并消费 headrelease；`reader`，api 用：只订 `ev.rocket.>`、发回执、有序读两条流；`cmd.>` 对两者都拒——2026-11-16 在本机同版本服务器上逐条验过），密码放 Secret（Owner 那份不读）；第 2 步起换 auth callout 按席位身份签（解第 30、31 条的顺序）。

## §11.1 谁在哪个环境订
- 测试环境的发布由**生产**平台 workers 里的 head-release 起（`HEAD_RELEASE=stg` 只在生产）。所以叫醒发布的 `git.main.advanced` 订阅在生产：Gitea 钩子要指生产平台，生产开总线。测试环境的账也收 git 事件，给测试环境自己的读口和验证用。


## §11.2 第 1 步的一次全程（2026-11-16，生产 `/api/v1/bus/trace?id=ev-131209`）
记账 14:19:38.193 → 推上 NATS +0.004 s → Console 的回执 +0.063 s → head-release 的回执 +0.001 s；测试环境的发布 9 秒后开始（head-release 在核对 main 上 CI 的结论）。main 动了到发布开始：51 秒 → 11 秒。

## §12 读数
每个主题的发布数、订阅数、落后量；中继落后量（账的最新游标 − 已推游标，按条数）；对账发现的偏差数；NATS 内存和盘；主干动了到测试环境开始发布的秒数；对 Gitea 的请求次数。
