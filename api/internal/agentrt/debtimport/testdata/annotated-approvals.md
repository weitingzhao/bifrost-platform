## 条目

### TD-293
**P2 · infra · The platform service accounts of STG and PROD can create, update and patch any ConfigMap in cicd, so either can lift the release freeze or rewrite the release window without the Owner's signature**
- 审批 要批（改集群 RBAC，apply_manifest）· 代价 M · 风险 med（漏一个名字会让 platform 写记录 403）· repos: bifrost-trade-infra

### TD-337
**P2 · ops-platform · routine platform releases still wait for the Owner's approval, and one pending request freezes the platform's STG: on 2026-10-13 three chained requests left the line idle for about nine hours**
- 审批 Owner 签规则（放宽）· 代价 L（W-56 的剩余门槛）· 风险 med · repos: bifrost-platform, bifrost-trade-infra

### TD-393
**P2 · agentrt · STG still waits for the Owner: a rule waits minutes, covers two card kinds, and leaves a card with no recommendation open**
- 审批 要（签 STG 规则 v3）· 代价 M · 风险 low（STG 只）· repos: bifrost-platform, bifrost-trade-infra

### TD-391
**P2 · agentrt · a card or approval reaches the Owner with no assessment of its content: a rule sees the card's fields, not that the work was already done elsewhere**
- 审批 不需要（Owner 14:0x 已定方向；存储若非加表不可再问）· 代价 M · 风险 medium（额度、注入）· repos: bifrost-platform
