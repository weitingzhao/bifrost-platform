# Progress — Daily Ops Checklist Step-2 + AI Check & Fix

**Status**: Row Fix + Ask for AI failover (2026-07-19)  
**META**: `DAILY_OPS_CHECKLIST_META.version = 2026-07-19-row-fix-ask-ai` · `DAILY_OPS_FLEET_DESK.version = 2026-07-19-row-fix-ask-ai`

## Step-2 Waves (prior)

| Wave | Status | Notes |
|------|--------|-------|
| 1.1 Probe | ✅ | Vendor Massive stable id `massive-polygon`; IB observe projection unchanged |
| 1.2 Prompts | ✅ | Massive / Data Layer / Operator HA prompts + `buildDispatchedFixPrompt` routing |
| 1.3 MCP/tools | ✅ | Runner tools wired; MCP `delete_pipeline_run` + checklist signal tools |
| 2 Checklist Agent | ✅ | Scope `daily-ops-checklist-run`, signals API, `report_checklist_signals`, launch script |
| 3 Auto-dispatch | ✅ | Gates + Go execute + Operate Queue `checklist_dispatch` + UI Action column |
| 4 KPI / escalation | ✅ | `/checklist/kpis`, quiet streak, AGENT_TASK_RELATIONS, protocol trust notes |

## Check & Fix MVP (Wave 0+1+2+4.2)

| Wave / Phase | Status | Notes |
|--------------|--------|-------|
| 0.1 Platform API URL notes | ✅ | Comments in `daily_ops_checklist.sh` + `deploy_mac_mini.sh` |
| 0.2 Naming lock | ✅ | AI Check = `daily-ops-checklist-run`; Operator Plane Fix stays `operator-plane-remediate` |
| 1.1 TCC AI Check button | ✅ | Header **AI Check** via `useAmbientAgentTask` |
| 1.2 Decouple from Operator Plane Fix | ✅ | Separate `onChecklistCheck` / `checklistCheck*` props |
| 1.3 Invalidate + faster poll | ✅ | On Check done/failed invalidate signals/kpis/jobs |
| 2.1 checklistProgress.ts | ✅ | Derive progress states + tests |
| 2.2 DispatchActionBadge | ✅ | Live labels + job open |
| 2.3 Header progress strip | ✅ | Prober · Xs + Dispatch strip |
| 4.2 Skip semantics | ✅ | `Skip · dedup 24h` / `Skip · D10` |

## Follow-up (Wave 3.1 + 4.1 + 4.3)

| Wave / Phase | Status | Notes |
|--------------|--------|-------|
| 3.1 Dual-source hint | ✅ | Notes `fleet≠agent` when agent vs fleet polarity disagrees; lamps stay fleet |
| 4.1 Row follow job / Open Queue | ✅ | Auto→open job; Queue→Control Room; notify/manual→`manualAction` in Notes |
| 4.3 Busy demote visibility | ✅ | `Queued (busy)` from concurrent-auto demote detail; Go enqueue preserves demote reason |

## Row Fix + Ask for AI (Cursor failover)

| Item | Status | Notes |
|------|--------|-------|
| Per-row **Fix** | ✅ | full_auto/semi_auto + fixScope → ambient `startRemediation`; observe/manual null-scope → no Fix |
| Per-row **Ask AI** | ✅ | Copy Cursor failover pack for that item |
| Header **Ask for AI (N)** | ✅ | Pack all non-ok items |
| Prompt builder | ✅ | `checklistCursorFailoverPrompt.ts` |
| Protocol Q12 | ✅ | META `2026-07-19-row-fix-ask-ai` |

### Deferred

| Wave | Status | Notes |
|------|--------|-------|
| 3.2 Full signals→lamp merge | ⏸ | Explicitly skipped |
| 5 SSE / per-item prober progress | ⏸ | Out of plan |

## Key paths

- `console/src/lib/control-room/checklistProgress.ts`
- `console/src/lib/control-room/checklistCursorFailoverPrompt.ts`
- `console/src/lib/control-room/checklistDispatch.ts`
- `console/src/components/task-mode/DailyOpsOperatorPlanPanel.tsx`
- `api/internal/checklist/dispatch.go` — preserve busy demote detail on enqueue
- `scripts/agent/daily_ops_checklist.sh`
