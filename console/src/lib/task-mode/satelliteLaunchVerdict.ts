import type { DeliveryPipelineRunView } from '@/api/deliveryTypes'
import { isPipelineRunRunning } from '@/lib/delivery/pipelineRunAskPack'
import type { Signal } from '@/lib/control-room/missionSignals'

/**
 * Live launch gate for Task CC — not playbook / last-deliver sticky state.
 * PROBING: a readiness dimension is not measured yet (or its probe returned
 * nothing) — Launch stays disabled; unknown never reads as clear (TD-226).
 */
export type LaunchVerdictKind = 'GO' | 'NO_GO' | 'IN_FLIGHT' | 'PROBING'

export type LaunchBlockKind = 'rocket' | 'prod' | 'both' | 'auth' | null

export type LaunchVerdict = {
  kind: LaunchVerdictKind
  title: string
  detail: string
  /** Why Agent Deploy / Launch is disabled (when not GO). */
  disabledReason?: string
  blockKind?: LaunchBlockKind
}

export type ResolveLaunchVerdictInput = {
  canOperate: boolean
  prodBlocked: boolean
  /** Satellite: rocket / prod / both. Rocket mode may omit. */
  blockKind?: Exclude<LaunchBlockKind, 'auth'>
  rocketDetail?: string
  tradeProdLabel?: string
  rocketLabel?: string
  /** Raw Signal for the trade-prod readiness dimension. */
  tradeProdSignal?: Signal
  /** Raw Signal for the rocket readiness dimension. */
  rocketSignal?: Signal
  /** Promote / cutover verify — gates prod-path launch when not ready. */
  promoteSignal?: Signal
  promoteDetail?: string
  deliverInFlight: boolean
  /**
   * Ambient deploy agent already started (scope trade-deploy / release) but Tekton
   * PipelineRun may not exist yet — still treat as in-flight so Launch Live View can open.
   */
  agentInFlight?: boolean
  mode: 'satellite' | 'rocket'
}

export type LaunchCheckpointId =
  | 'auth'
  | 'rocket'
  | 'trade-prod'
  | 'platform-prod'
  | 'promote'
  | 'pipeline'
  | 'tag'
  /** @deprecated collapsed readiness — prefer rocket / trade-prod */
  | 'readiness'

/** DOM id suffix under Environment readiness / Recent launches for scroll-from-Launch. */
export type LaunchReadinessAnchor = 'rocket' | 'trade-prod' | 'platform-prod' | 'pipeline' | 'stg'

export type LaunchCheckpoint = {
  id: LaunchCheckpointId
  label: string
  ok: boolean
  /** Actual severity — lets the lamp show degraded (yellow) vs fail (red). */
  signal?: Signal
  detail?: string
  /** Scroll target in the summary row — 1:1 with Environment Readiness / Recent panels. */
  readinessAnchor?: LaunchReadinessAnchor
}

/** True if any PipelineRun in the list is still running. */
export function hasDeliverInFlight(runs: DeliveryPipelineRunView[] | undefined): boolean {
  return (runs ?? []).some(r => isPipelineRunRunning(r))
}

/** Map live verdict to StatusLamp / icon color — not last-deliver health. */
export function launchVerdictToSignal(kind: LaunchVerdictKind): Signal {
  if (kind === 'GO') return 'ok'
  if (kind === 'IN_FLIGHT') return 'degraded'
  if (kind === 'PROBING') return 'unknown'
  return 'fail'
}

/** Short tag text for a verdict kind (GO / IN FLIGHT / PROBING / NO-GO). */
export function launchVerdictLabel(kind: LaunchVerdictKind): string {
  if (kind === 'GO') return 'GO'
  if (kind === 'IN_FLIGHT') return 'IN FLIGHT'
  if (kind === 'PROBING') return 'PROBING'
  return 'NO-GO'
}

/** DenseTag variant for a verdict kind: PROBING is a warning, not a red NO-GO. */
export function launchVerdictTagVariant(kind: LaunchVerdictKind): 'success' | 'warning' | 'danger' {
  if (kind === 'GO') return 'success'
  if (kind === 'IN_FLIGHT' || kind === 'PROBING') return 'warning'
  return 'danger'
}

function signalBlocksLaunch(s: Signal | undefined): boolean {
  return s === 'fail' || s === 'degraded'
}

/**
 * A readiness dimension that has not reported 'ok' or a failure. Missing
 * (undefined) counts as unmeasured too: Launch is clear only on a measured
 * 'ok' (TD-226).
 */
function signalUnmeasured(s: Signal | undefined): boolean {
  return s == null || s === 'unknown'
}

const NOT_MEASURED = 'readiness not measured'

/** Checkpoint fields for one readiness dimension: ok only on a measured 'ok'. */
function readinessCheckpointFields(
  s: Signal | undefined,
  blockedDetail: string | undefined,
): Pick<LaunchCheckpoint, 'ok' | 'signal' | 'detail'> {
  return {
    ok: s === 'ok',
    signal: s ?? 'unknown',
    detail: signalBlocksLaunch(s) ? blockedDetail : signalUnmeasured(s) ? NOT_MEASURED : undefined,
  }
}

/** Readiness dimensions that gate this lane (by mode). */
function readinessSignals(input: ResolveLaunchVerdictInput): (Signal | undefined)[] {
  return input.mode === 'satellite'
    ? [input.rocketSignal, input.tradeProdSignal, input.promoteSignal]
    : [input.tradeProdSignal, input.promoteSignal]
}

export function readinessAnchorDomId(anchor: LaunchReadinessAnchor): string {
  return `task-cc-readiness-${anchor}`
}

/**
 * Critical launch checkpoints — lamps map 1:1 onto Environment Readiness / Recent panels.
 * Satellite: Auth · Rocket IB bus · Trade Prod · Promote / cutover · Pipeline idle
 * Rocket: Auth · Platform Prod · Promote / cutover · Pipeline idle
 * Verdict title (e.g. "Fix Prod environment before release") is not a checkpoint —
 * lane pages count these arrays for the `N/M ready` tag.
 * Any `ok: false` ⇒ not GO (same inputs as resolveLaunchVerdict). A readiness
 * checkpoint is ok only on a measured 'ok' signal — 'unknown' / missing is not
 * green (TD-226).
 */
export function buildLaunchCheckpoints(input: ResolveLaunchVerdictInput): LaunchCheckpoint[] {
  const authOk = input.canOperate
  const pipelineOk = !input.deliverInFlight
  const promote = readinessCheckpointFields(input.promoteSignal, input.promoteDetail)

  if (input.mode === 'satellite') {
    return [
      { id: 'auth', label: 'Operator auth', ok: authOk },
      {
        id: 'rocket',
        label: 'Rocket IB bus',
        ...readinessCheckpointFields(input.rocketSignal, input.rocketLabel),
        readinessAnchor: 'rocket',
      },
      {
        id: 'trade-prod',
        label: 'Trade Prod',
        ...readinessCheckpointFields(input.tradeProdSignal, input.tradeProdLabel),
        readinessAnchor: 'trade-prod',
      },
      {
        id: 'promote',
        label: 'Promote / cutover',
        ...promote,
        readinessAnchor: 'trade-prod',
      },
      {
        id: 'pipeline',
        label: 'Pipeline idle',
        ok: pipelineOk,
        readinessAnchor: 'pipeline',
      },
    ]
  }

  const platformBlocked = input.prodBlocked
  return [
    { id: 'auth', label: 'Operator auth', ok: authOk },
    {
      id: 'platform-prod',
      label: 'Platform Prod',
      ...(platformBlocked
        ? {
            ok: false,
            signal: input.tradeProdSignal ?? input.rocketSignal ?? 'fail',
            detail: input.tradeProdLabel ?? input.rocketLabel,
          }
        : readinessCheckpointFields(input.tradeProdSignal, input.tradeProdLabel)),
      readinessAnchor: 'platform-prod',
    },
    {
      id: 'promote',
      label: 'Promote / cutover',
      ...promote,
      readinessAnchor: 'platform-prod',
    },
    {
      id: 'pipeline',
      label: 'Pipeline idle',
      ok: pipelineOk,
      readinessAnchor: 'pipeline',
    },
  ]
}

/**
 * Resolve live Go / No-Go / In-flight / Probing for Satellite or Rocket Task CC.
 * Priority: auth/env NO_GO → IN_FLIGHT → PROBING (a readiness signal not
 * measured) → GO.
 */
export function resolveLaunchVerdict(input: ResolveLaunchVerdictInput): LaunchVerdict {
  if (!input.canOperate) {
    return {
      kind: 'NO_GO',
      title: 'Authenticate before launch',
      detail: 'Use the header auth control as operator before starting Launch Pad agents.',
      disabledReason: 'Authenticate as operator to run Launch Pad agents',
      blockKind: 'auth',
    }
  }

  if (signalBlocksLaunch(input.promoteSignal)) {
    return {
      kind: 'NO_GO',
      title: 'Complete Promote / cutover verify before launch',
      detail:
        input.promoteDetail != null && input.promoteDetail !== ''
          ? input.promoteDetail
          : 'Spine promote / prod cutover verify is not clear — resolve blockers before Agent Launch.',
      disabledReason: 'Promote / cutover verify blocked',
      blockKind: 'prod',
    }
  }

  if (input.prodBlocked) {
    if (input.mode === 'satellite') {
      const kind = input.blockKind ?? 'prod'
      if (kind === 'rocket') {
        return {
          kind: 'NO_GO',
          title: 'Fix Rocket IB bus before release',
          detail: `Shared Rocket readiness is ${input.rocketLabel ?? 'blocked'}${
            input.rocketDetail != null && input.rocketDetail !== ''
              ? ` (${input.rocketDetail})`
              : ''
          } — resolve Platform IB Gateway / socket consumers before deploying.`,
          disabledReason: 'Rocket IB bus blocked — fix shared gateway before deploy',
          blockKind: 'rocket',
        }
      }
      if (kind === 'both') {
        return {
          kind: 'NO_GO',
          title: 'Fix Rocket IB bus and Prod environment before release',
          detail: `Rocket: ${input.rocketLabel ?? 'blocked'}${
            input.rocketDetail != null && input.rocketDetail !== ''
              ? ` — ${input.rocketDetail}`
              : ''
          }. Trade Prod: ${input.tradeProdLabel ?? 'blocked'} — resolve workloads, datastore, or API reachability.`,
          disabledReason: 'Rocket IB bus and Trade Prod readiness blocked',
          blockKind: 'both',
        }
      }
      return {
        kind: 'NO_GO',
        title: 'Fix Prod environment before release',
        detail: `Trade Prod readiness is ${input.tradeProdLabel ?? 'blocked'} — resolve Prod workloads, datastore, or API reachability before deploying.`,
        disabledReason: 'Prod readiness blocked — fix environment first',
        blockKind: 'prod',
      }
    }

    return {
      kind: 'NO_GO',
      title: 'Fix Prod environment before release',
      detail: `Platform Prod readiness is ${input.tradeProdLabel ?? input.rocketLabel ?? 'blocked'} — resolve Prod namespace, self-health, or release gate issues before launching release agents.`,
      disabledReason: 'Prod readiness blocked — fix environment first',
      blockKind: 'prod',
    }
  }

  if (input.deliverInFlight || input.agentInFlight) {
    const agentOnly = input.agentInFlight === true && !input.deliverInFlight
    return {
      kind: 'IN_FLIGHT',
      title: 'Launch in progress',
      detail: agentOnly
        ? input.mode === 'satellite'
          ? 'Deploy Satellite agent is running — waiting for the bifrost-deliver-stg PipelineRun to appear.'
          : 'Launch Rocket agent is running — waiting for the bifrost-deliver-platform PipelineRun to appear.'
        : input.mode === 'satellite'
          ? 'A bifrost-deliver-stg PipelineRun is still running — wait for it to finish or open Deploy Satellite for logs.'
          : 'A bifrost-deliver-platform PipelineRun is still running — wait for it to finish or open Launch Rocket for logs.',
      disabledReason: 'Launch already in progress',
    }
  }

  if (readinessSignals(input).some(signalUnmeasured)) {
    return {
      kind: 'PROBING',
      title: 'Readiness not measured',
      detail:
        'A readiness probe has not answered yet (or returned nothing) — Launch stays disabled until every dimension reports.',
      disabledReason: 'readiness not measured',
    }
  }

  return {
    kind: 'GO',
    title: 'Clear to launch',
    detail:
      input.mode === 'satellite'
        ? 'Live readiness is clear and no deliver-stg run is in flight. Agent Deploy starts a new launch.'
        : 'Live readiness is clear and no platform deliver run is in flight. Agent Launch starts a new release.',
  }
}
