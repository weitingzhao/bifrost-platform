/**
 * Daily Ops Task Control Center — Discover → Remediate → Verify → Clear.
 * Pure resolver; Fleet board remains health ground truth.
 *
 * Remediate primary CTA follows the highest-priority Checklist blocker:
 * - full_auto / semi_auto + fixScope → Agent Fix / AI Fix · Operator Plan
 * - manual / observe / null scope → Manual next step (no sparkles AI Fix)
 */
import {
  cellAllowsAgentFix,
  lookupFleetFixRoute,
  pickFleetFixCell,
} from '@/lib/control-room/fleetCellFix'
import {
  blockerAllowsAiFix,
  blockerRequiresManualPath,
  collectDailyOpsBlockers,
  gitDirtyPrimaryCtaLabel,
  isGitDirtyBlocker,
  manualPrimaryCtaLabel,
  nextStepBanner,
  pickPrimaryBlocker,
  pickSecondaryAiBlocker,
  secondaryAiCtaLabel,
  type DailyOpsBlocker,
} from '@/lib/control-room/dailyOpsPrimaryBlocker'
import {
  resolveCellGate,
  type FleetSnapshot,
} from '@/lib/control-room/fleetSnapshot'

export type DailyOpsWorkflowPhase = 'discover' | 'remediate' | 'verify' | 'clear'

export type DailyOpsWorkflowPrimaryAction = {
  /**
   * operator-plan — Engineer escalate: stay on TCC, show inline Operator Plan + AI Fix.
   * Full Operator Plane page is escape hatch only (secondary link).
   * ai-check — Discover primary: Checklist probe (daily-ops-checklist-run).
   * run-check — Clear idle re-check (same Checklist probe; label differs).
   * manual-next — Physical / observe / null-scope blocker — not Agent Fix.
   */
  kind:
    | 'agent-fix'
    | 'operator-plan'
    | 'propose-commit'
    | 'manual-next'
    | 'navigate'
    | 'verify'
    | 'clear-queue'
    | 'run-check'
    | 'ai-check'
    | 'view-agent'
    | 'none'
  label: string
  tabId?: string
  cellKey?: string
  /** Checklist item driving this CTA (when derived from blockers). */
  blockerItemId?: string
  /** Copy / toast hint for manual-next. */
  manualHint?: string
  /**
   * Outline/muted secondary when primary is manual but an AI-fixable sibling exists.
   * e.g. Also: Propose commit (git dirty)
   */
  secondary?: {
    kind: 'operator-plan' | 'agent-fix' | 'propose-commit'
    label: string
    cellKey?: string
    blockerItemId?: string
  }
}

export type DailyOpsWorkflowResult = {
  activePhase: DailyOpsWorkflowPhase
  blockers: string[]
  primaryAction: DailyOpsWorkflowPrimaryAction
  targetCellKey?: string
  /** Structured primary blocker (Checklist × Fleet) when on remediate. */
  primaryBlocker?: DailyOpsBlocker
}

export type DailyOpsStepStatus = 'done' | 'active' | 'blocked' | 'planned'

export const DAILY_OPS_WORKFLOW_PHASES: ReadonlyArray<{
  id: DailyOpsWorkflowPhase
  label: string
}> = [
  { id: 'discover', label: 'Discover' },
  { id: 'remediate', label: 'Remediate' },
  { id: 'verify', label: 'Verify' },
  { id: 'clear', label: 'Clear' },
]

export type ResolveDailyOpsWorkflowInput = {
  fleet: FleetSnapshot
  agentPending?: boolean
  agentJustSucceeded?: boolean
  queueOpen?: number
}

function engineerEscalateCell(fleet: FleetSnapshot) {
  const worst = fleet.verdict.worstCell
  if (worst == null) return null
  if (worst.role !== 'engineer') return null
  if (resolveCellGate(worst) === 'GO') return null
  const env = worst.span ? 'span' : (worst.env ?? 'span')
  const route = lookupFleetFixRoute(worst.role, env)
  if (route?.navigateTabId == null && worst.escalateTabId == null) return null
  return worst
}

function d10Blockers(): string[] {
  return ['D10 live trading remains BLOCKED — no Agent Fix for trade execution unlock.']
}

function remediateFromEngineer(
  fleet: FleetSnapshot,
  eng: NonNullable<ReturnType<typeof engineerEscalateCell>>,
): DailyOpsWorkflowResult {
  const blockers = d10Blockers()
  const tabId =
    eng.escalateTabId ??
    lookupFleetFixRoute('engineer', 'span')?.navigateTabId ??
    'operator-plane'

  const engBlockers = collectDailyOpsBlockers(fleet, { cell: eng })
  const primary = pickPrimaryBlocker(engBlockers)

  if (primary != null && blockerRequiresManualPath(primary)) {
    const secondaryAi = pickSecondaryAiBlocker(engBlockers, primary)
    blockers.push(nextStepBanner(primary))
    const secondary =
      secondaryAi != null
        ? isGitDirtyBlocker(secondaryAi)
          ? {
              kind: 'propose-commit' as const,
              label: secondaryAiCtaLabel(secondaryAi),
              cellKey: eng.key,
              blockerItemId: secondaryAi.itemId,
            }
          : {
              kind: 'operator-plan' as const,
              label: secondaryAiCtaLabel(secondaryAi),
              cellKey: eng.key,
              blockerItemId: secondaryAi.itemId,
            }
        : undefined
    return {
      activePhase: 'remediate',
      blockers,
      primaryAction: {
        kind: 'manual-next',
        label: manualPrimaryCtaLabel(primary),
        tabId,
        cellKey: eng.key,
        blockerItemId: primary.itemId,
        manualHint: primary.manualAction ?? primary.reason,
        secondary,
      },
      targetCellKey: eng.key,
      primaryBlocker: primary,
    }
  }

  // Git dirty primary → Propose commit (approval path), not magic AI Fix
  if (primary != null && isGitDirtyBlocker(primary)) {
    blockers.push(nextStepBanner(primary))
    return {
      activePhase: 'remediate',
      blockers,
      primaryAction: {
        kind: 'propose-commit',
        label: gitDirtyPrimaryCtaLabel(primary),
        tabId,
        cellKey: eng.key,
        blockerItemId: primary.itemId,
        secondary: {
          kind: 'propose-commit',
          label: 'Also: Stash to clear Fleet',
          cellKey: eng.key,
          blockerItemId: primary.itemId,
        },
      },
      targetCellKey: eng.key,
      primaryBlocker: primary,
    }
  }

  // AI-fixable primary (or no catalog match) → Operator Plan AI Fix
  if (primary != null) {
    blockers.push(nextStepBanner(primary))
  } else {
    blockers.push(
      eng.agentFixDisabledReason ??
        'Engineer CRITICAL — review Operator Plan in-place (fleet cell Agent Fix disabled)',
    )
  }

  return {
    activePhase: 'remediate',
    blockers,
    primaryAction: {
      kind: 'operator-plan',
      label: 'AI Fix · Operator Plan',
      tabId,
      cellKey: eng.key,
      blockerItemId: primary?.itemId,
    },
    targetCellKey: eng.key,
    primaryBlocker: primary ?? undefined,
  }
}

/**
 * Resolve the pinned Daily Ops workflow phase from fleet + agent/queue signals.
 *
 * Priority:
 * 1. fleetClear && queueOpen===0 → clear (Run daily check / Checklist re-probe)
 * 2. agentPending → remediate (View agent)
 * 3. agentJustSucceeded && !fleetClear → verify
 * 4. fleetClear && queueOpen>0 → clear
 * 5. !fleetClear && fixable → remediate (agent-fix)
 * 6. !fleetClear && engineer escalate → remediate (manual-next | operator-plan by blocker)
 * 7. !fleetClear → discover (AI Check — daily-ops-checklist-run)
 */
export function resolveDailyOpsWorkflow(
  input: ResolveDailyOpsWorkflowInput,
): DailyOpsWorkflowResult {
  const { fleet, agentPending = false, agentJustSucceeded = false } = input
  const queueOpen = input.queueOpen ?? 0
  const blockers = d10Blockers()

  if (fleet.fleetClear && queueOpen === 0) {
    return {
      activePhase: 'clear',
      blockers: [],
      primaryAction: { kind: 'run-check', label: 'Run daily check' },
    }
  }

  if (agentPending) {
    const fixCell = pickFleetFixCell(fleet)
    const cellKey =
      fleet.verdict.primaryCta.cellKey ?? fixCell?.key ?? fleet.verdict.worstCell?.key
    // Keep primary blocker while Agent runs so Execution → Now can compare
    // Running (job) vs Loop next (same catalog target as pre-flight CTA).
    const pendingBlockers =
      fixCell != null
        ? collectDailyOpsBlockers(fleet, { cell: fixCell })
        : (() => {
            const eng = engineerEscalateCell(fleet)
            return eng != null
              ? collectDailyOpsBlockers(fleet, { cell: eng })
              : collectDailyOpsBlockers(fleet)
          })()
    const primary = pickPrimaryBlocker(pendingBlockers)
    return {
      activePhase: 'remediate',
      blockers,
      primaryAction: {
        kind: 'view-agent',
        label: 'View agent',
        tabId: 'agent-desk',
        cellKey,
        blockerItemId: primary?.itemId,
      },
      targetCellKey: cellKey,
      primaryBlocker: primary ?? undefined,
    }
  }

  if (agentJustSucceeded && !fleet.fleetClear) {
    const worstKey = fleet.verdict.worstCell?.key
    return {
      activePhase: 'verify',
      blockers,
      primaryAction: { kind: 'verify', label: 'Re-probe fleet' },
      targetCellKey: worstKey,
    }
  }

  if (fleet.fleetClear && queueOpen > 0) {
    return {
      activePhase: 'clear',
      blockers: [],
      primaryAction: {
        kind: 'clear-queue',
        label: `Clear queue (${queueOpen})`,
        tabId: 'control-room',
      },
    }
  }

  // NO-GO path — prefer Remediate when a worst / fixable cell is ready
  const fixCell = pickFleetFixCell(fleet)
  if (fixCell != null && cellAllowsAgentFix(fixCell)) {
    const cellBlockers = collectDailyOpsBlockers(fleet, { cell: fixCell })
    const primary = pickPrimaryBlocker(cellBlockers)
    // Rare: fixable cell whose primary catalog item is actually manual — defer to manual CTA
    if (primary != null && blockerRequiresManualPath(primary) && !blockerAllowsAiFix(primary)) {
      blockers.push(nextStepBanner(primary))
      return {
        activePhase: 'remediate',
        blockers,
        primaryAction: {
          kind: 'manual-next',
          label: manualPrimaryCtaLabel(primary),
          cellKey: fixCell.key,
          blockerItemId: primary.itemId,
          manualHint: primary.manualAction ?? primary.reason,
        },
        targetCellKey: fixCell.key,
        primaryBlocker: primary,
      }
    }
    return {
      activePhase: 'remediate',
      blockers,
      primaryAction: {
        kind: 'agent-fix',
        label: 'Agent Fix',
        cellKey: fixCell.key,
        blockerItemId: primary?.itemId,
      },
      targetCellKey: fixCell.key,
      primaryBlocker: primary ?? undefined,
    }
  }

  const eng = engineerEscalateCell(fleet)
  if (eng != null) {
    return remediateFromEngineer(fleet, eng)
  }

  if (!fleet.fleetClear) {
    const worst = fleet.verdict.worstCell
    // Stage-driven single primary CTA: Discover always owns AI Check on the process strip.
    // Navigate / cell Fix remain available from Fleet Board — not competing strip CTAs.
    return {
      activePhase: 'discover',
      blockers,
      primaryAction: {
        kind: 'ai-check',
        label: 'AI Check',
        cellKey: worst?.key,
      },
      targetCellKey: worst?.key,
    }
  }

  // Fallback — should be unreachable given clear rules above
  return {
    activePhase: 'clear',
    blockers: [],
    primaryAction: { kind: 'run-check', label: 'Run daily check' },
  }
}

/** Phase index for stepper highlighting (0–3). */
export function dailyOpsWorkflowPhaseIndex(phase: DailyOpsWorkflowPhase): number {
  return DAILY_OPS_WORKFLOW_PHASES.findIndex(p => p.id === phase)
}

/** Circle-stepper statuses aligned with TaskPhaseProgress / FlowStepper. */
export function dailyOpsStepStatuses(
  workflow: DailyOpsWorkflowResult,
): Record<DailyOpsWorkflowPhase, DailyOpsStepStatus> {
  const activeIdx = dailyOpsWorkflowPhaseIndex(workflow.activePhase)
  const allClear =
    workflow.activePhase === 'clear' &&
    (workflow.primaryAction.kind === 'run-check' || workflow.primaryAction.kind === 'none')
  const out = {} as Record<DailyOpsWorkflowPhase, DailyOpsStepStatus>
  for (const [idx, phase] of DAILY_OPS_WORKFLOW_PHASES.entries()) {
    if (allClear) {
      out[phase.id] = 'done'
      continue
    }
    if (idx < activeIdx) {
      out[phase.id] = 'done'
      continue
    }
    if (idx === activeIdx) {
      // operator-plan / agent-fix / manual-next / view-agent are actionable remediate — active
      out[phase.id] = 'active'
      continue
    }
    out[phase.id] = 'planned'
  }
  return out
}
