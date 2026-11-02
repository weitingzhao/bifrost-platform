## 待你签收

- **TD-392** — 运行时连接池按环境设上限（PROD 每进程 4、STG/DEV 每进程 2），两个进程加起来低于库账号的连接上限，发版换 Pod 不再连不上库（platform `4986a68b`，#55 上 PROD）。验收：PASS 2026-10-20，PROD 换 Pod 0 次拒绝、连接 6/10。防线：`RATCHETS.md`「两个进程的连接池低于账号上限」。无后续：压到每池 2 个跑过运行时各包，没有嵌套占连接。
- **TD-384** — 预算用完的任务挂起出卡，「再来一轮」按这一轮放出预算并重新派出（platform `eb40efe6` 等，#53 上 PROD）。验收：PASS 2026-10-20，PROD M-9 出卡写明预算、STG M-39 选 a 后 30 秒内被领走。防线：`RATCHETS.md`「预算用完不困住任务」。无后续：STG 上旧走查的残留已由 TD-389 清掉。
- **TD-378** — 准入守卫检查的测试在 main 上一直失败：假的 `subprocess.run` 改为也回答 Application 读取（infra `d989dde`，运行时 M-9 写），文件进运行时的 infra 检查（platform `6b950732`）。验收：PASS 2026-10-20，单文件 10 passed，整份清单 259 passed。防线：`RATCHETS.md`「准入守卫的测试随每件 infra 任务跑」。无后续：清单里别的排除项各有原因，写在 `repos.go` 注释里。

## 条目

### TD-392

**P2 · agentrt · the runtime's pools have no size: two processes sit at the role's connection limit, and a restarted one is refused for up to a minute on every release**

- **状态**：待你签收（2026-10-20 18:5x；platform `4986a68b`，随 #55 上 PROD）

### TD-384

**P2 · agentrt · a task the mission's budget will not let run waits in ready with no event and no card, and a card's "Give it one more round" can land it there**

- **状态**：待你签收（2026-10-20 16:2x；修复 platform `eb40efe6` 等，随 #53 上 PROD）

### TD-378

**P3 · testing · a test of the admission-guard check fails on main: its fake `subprocess.run` answers the Application read too**

- **状态**：待你签收（2026-10-20 13:0x：修复 infra `d989dde`（运行时 PROD M-9 收下、统筹线程手工合进）；防线由统筹线程补上：文件进 `infraTests`，`repos_test.go` 改为要求它在清单里，platform `6b950732`）
