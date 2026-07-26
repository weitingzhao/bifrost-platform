import { useCallback, useEffect, useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Button, DenseTag, cn } from '@bifrost/ui'
import { ExternalLink, Loader2, Wrench } from 'lucide-react'
import { postIbGatewayControl } from '@/api/network'
import { rolloutRestartDeployment } from '@/api/clusterActuation'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import type { ProdFixSignal } from '@/lib/agent/prodEnvironmentFixPrompt'
import type { Signal } from '@/lib/control-room/missionSignals'
import { upsertActivity, updateActivityPhase } from '@/lib/activity/activityStore'
import type { ActivitySettledOutcome } from '@/lib/activity/activityTypes'
import { chipCorrelateKey } from '@/lib/activity/signalTransitionDetector'
import {
  readinessChipFixActions,
  setSatelliteApiEnv,
  setSatelliteBusFocus,
  type ReadinessChipAction,
  type ReadinessChipContext,
} from '@/lib/task-mode/readinessChipActions'
import { pickPrimaryFailingChip } from '@/components/task-mode/readiness/utils'

/**
 * FixBar four-beat lifecycle (W0):
 *   idle → requesting → applied → settled (resolved | signal-unchanged | timeout | error)
 *
 * STG/PROD: actuations target bifrost-stg / bifrost-prod namespaces from readinessChipActions.
 * D10: never scale daemon up — FixBar only exposes rollout-restart + ib-gateway-reconnect.
 */

const SETTLE_POLL_MS = 5_000
const SETTLE_TIMEOUT_MS = 30_000

type FixBarBeat = 'idle' | 'requesting' | 'applied' | 'settled'

type SettleWatch = {
  activityId: string
  chipLabelIncludes: string
  actionLabel: string
  navigateAction: ReadinessChipAction | null
  startedAt: number
}

type ReadinessFixBarProps = {
  chips: Array<{ label: string; signal: Signal; detail: string }>
  ctx: ReadinessChipContext
  canOperate: boolean
  onNavigate: (tabId: string) => void
  onAgentFix?: () => void
  agentFixPending?: boolean
  agentFixDisabled?: boolean
  agentFixTitle?: string
  dense?: boolean
}

function runActuation(action: ReadinessChipAction) {
  const act = action.actuation
  if (act == null) return Promise.reject(new Error('No actuation configured'))
  if (act.kind === 'rollout-restart') {
    return rolloutRestartDeployment({
      namespace: act.namespace,
      kind: 'Deployment',
      name: act.deployment,
    })
  }
  if (act.kind === 'ib-gateway-reconnect') {
    return postIbGatewayControl('reconnect')
  }
  return Promise.reject(new Error('Unknown actuation'))
}

function findWatchedChip(
  chips: Array<{ label: string; signal: Signal; detail: string }>,
  includes: string,
) {
  const needle = includes.toLowerCase()
  return chips.find(c => c.label.toLowerCase().includes(needle))
}

function actuationTargetLabel(action: ReadinessChipAction): string {
  const act = action.actuation
  if (act?.kind === 'rollout-restart') return `${act.namespace}/${act.deployment}`
  if (act?.kind === 'ib-gateway-reconnect') return 'ib-gateway'
  return action.label
}

function settledMessage(outcome: ActivitySettledOutcome, detail: string): string {
  switch (outcome) {
    case 'resolved':
      return `Settled · resolved — ${detail}`
    case 'signal-unchanged':
      return `Settled · signal unchanged — ${detail}`
    case 'timeout':
      return `Settled · timeout — ${detail}`
    case 'error':
      return `Settled · error — ${detail}`
  }
}

export function ReadinessFixBar({
  chips,
  ctx,
  canOperate,
  onNavigate,
  onAgentFix,
  agentFixPending = false,
  agentFixDisabled = false,
  agentFixTitle,
  dense = false,
}: ReadinessFixBarProps) {
  const qc = useQueryClient()
  const [beat, setBeat] = useState<FixBarBeat>('idle')
  const [statusMsg, setStatusMsg] = useState<string | null>(null)
  const [settledOutcome, setSettledOutcome] = useState<ActivitySettledOutcome | null>(null)
  const [settleNavigate, setSettleNavigate] = useState<ReadinessChipAction | null>(null)
  const watchRef = useRef<SettleWatch | null>(null)
  const inFlightRef = useRef(false)

  const failing = chips.filter(c => c.signal !== 'ok')
  const primaryFailing = pickPrimaryFailingChip(chips)
  const fixActions =
    primaryFailing != null
      ? readinessChipFixActions(primaryFailing.label, primaryFailing.signal, ctx)
      : []

  const handleNavigate = useCallback(
    (action: ReadinessChipAction) => {
      if (action.tabId == null) return
      if (action.tabId === 'satellite-bus') {
        setSatelliteBusFocus(action.busFocus)
      }
      if (action.tabId === 'satellite-api') {
        setSatelliteApiEnv(action.apiEnv)
      }
      onNavigate(action.tabId)
    },
    [onNavigate],
  )

  const finishSettle = useCallback(
    (outcome: ActivitySettledOutcome, detail: string, navigateHint?: ReadinessChipAction | null) => {
      const watch = watchRef.current
      const nav = navigateHint ?? watch?.navigateAction ?? null
      setBeat('settled')
      setSettledOutcome(outcome)
      setSettleNavigate(nav)
      setStatusMsg(settledMessage(outcome, detail))
      inFlightRef.current = false
      if (watch != null) {
        updateActivityPhase(watch.activityId, 'settled', {
          settledOutcome: outcome,
          detail,
          linkTo: nav?.tabId,
        })
      }
      watchRef.current = null
    },
    [],
  )

  // Settle watch: poll probes ~5s up to ~30s while applied.
  useEffect(() => {
    if (beat !== 'applied' || watchRef.current == null) return

    const tick = () => {
      const w = watchRef.current
      if (w == null) return
      const chip = findWatchedChip(chips, w.chipLabelIncludes)
      if (chip != null && chip.signal === 'ok') {
        finishSettle('resolved', `${chip.label} is ok`)
        return
      }
      if (Date.now() - w.startedAt >= SETTLE_TIMEOUT_MS) {
        const detail =
          chip != null
            ? `${chip.label} still ${chip.signal} after ${SETTLE_TIMEOUT_MS / 1000}s`
            : `No probe update for ${w.chipLabelIncludes} after ${SETTLE_TIMEOUT_MS / 1000}s`
        finishSettle(chip != null ? 'signal-unchanged' : 'timeout', detail, w.navigateAction)
      }
    }

    tick()
    const pollId = window.setInterval(() => {
      void qc.invalidateQueries({ queryKey: ['satellite'] })
      void qc.invalidateQueries({ queryKey: ['cluster'] })
      void qc.invalidateQueries({ queryKey: ['mission'] })
      tick()
    }, SETTLE_POLL_MS)

    return () => {
      window.clearInterval(pollId)
    }
  }, [beat, chips, qc, finishSettle])

  const actuateM = useMutation({
    mutationFn: async (action: ReadinessChipAction) => {
      const resp = await runActuation(action)
      if (!resp.ok) throw new Error(resp.message || 'Actuation failed')
      return resp
    },
    onMutate: action => {
      inFlightRef.current = true
      setBeat('requesting')
      setSettledOutcome(null)
      setSettleNavigate(null)
      const target = actuationTargetLabel(action)
      const chipIncludes =
        action.settleWatchTarget?.chipLabelIncludes ??
        primaryFailing?.label ??
        action.label
      const activityId = `actuation:${target}:${Date.now()}`
      const navigateAction =
        fixActions.find(a => a.kind === 'navigate' && a.tabId != null) ?? null
      watchRef.current = {
        activityId,
        chipLabelIncludes: chipIncludes,
        actionLabel: action.label,
        navigateAction,
        startedAt: Date.now(),
      }
      setStatusMsg('Requesting actuation…')
      const envScope = ctx.activityEnvScope ?? ctx.env
      upsertActivity({
        id: activityId,
        kind: 'actuation',
        phase: 'requested',
        title: action.label,
        target,
        detail: `Watching ${envScope}/${chipIncludes}`,
        linkTo: navigateAction?.tabId,
        correlateKey: chipCorrelateKey(envScope, chipIncludes),
        bumpTs: true,
      })
    },
    onSuccess: () => {
      setBeat('applied')
      setStatusMsg('Restart requested — monitoring probe…')
      const watch = watchRef.current
      if (watch != null) {
        watch.startedAt = Date.now()
        updateActivityPhase(watch.activityId, 'applying', {
          detail: 'Restart requested — monitoring probe…',
        })
      }
    },
    onError: (e: Error) => {
      finishSettle('error', e.message)
    },
  })

  const busy = beat === 'requesting' || beat === 'applied' || actuateM.isPending
  const showBar = failing.length > 0 || busy || beat === 'settled'
  if (!showBar) return null
  if (primaryFailing == null && !busy && beat !== 'settled') return null

  const chipLabel = primaryFailing?.label ?? watchRef.current?.chipLabelIncludes ?? 'Fix'
  const chipDetail = primaryFailing?.detail ?? statusMsg ?? ''

  return (
    <div
      className={cn(
        'rounded-md border border-warning/30 bg-warning/5',
        dense ? 'mt-1.5 px-2 py-1.5' : 'mt-2 px-2.5 py-2',
      )}
    >
      <div className="flex flex-wrap items-center gap-1.5">
        <Wrench size={dense ? 12 : 14} className="shrink-0 text-warning" aria-hidden />
        <span className={cn('font-medium text-warning', dense ? 'text-[9px]' : 'text-[var(--text-dense-caption)]')}>
          Fix · {chipLabel}
        </span>
        {beat !== 'idle' && (
          <DenseTag
            variant={
              beat === 'settled'
                ? settledOutcome === 'resolved'
                  ? 'success'
                  : settledOutcome === 'error'
                    ? 'danger'
                    : 'warning'
                : 'info'
            }
            className="text-[9px] uppercase tracking-wide"
          >
            {beat === 'requesting'
              ? 'requesting'
              : beat === 'applied'
                ? 'applied'
                : settledOutcome ?? 'settled'}
          </DenseTag>
        )}
      </div>
      {chipDetail !== '' && beat === 'idle' && (
        <p className={cn('m-0 text-muted-foreground', dense ? 'mt-0.5 text-[9px]' : 'mt-1 text-[var(--text-dense-caption)]')}>
          {chipDetail}
        </p>
      )}
      <div className={cn('flex flex-wrap items-center gap-1.5', dense ? 'mt-1' : 'mt-1.5')}>
        {fixActions.map(action => {
          const needsAuth = action.requiresOperate === true
          const disabled = needsAuth && !canOperate
          if (action.kind === 'navigate') {
            return (
              <Button
                key={`${action.label}-${action.tabId}`}
                type="button"
                variant="outline"
                size="xs"
                className="h-6 gap-1 text-[10px]"
                onClick={() => handleNavigate(action)}
              >
                <ExternalLink size={10} aria-hidden />
                {action.label}
              </Button>
            )
          }
          return (
            <Button
              key={action.label}
              type="button"
              variant="secondary"
              size="xs"
              className="h-6 gap-1 text-[10px]"
              disabled={disabled || busy}
              title={
                disabled
                  ? 'Operator authentication required'
                  : busy
                    ? 'Actuation in flight — wait for settle'
                    : action.label
              }
              onClick={() => {
                if (inFlightRef.current || busy) return
                actuateM.mutate(action)
              }}
            >
              {beat === 'requesting' ? <Loader2 size={10} className="animate-spin" /> : null}
              {action.label}
            </Button>
          )
        })}
        {onAgentFix != null && (
          <AgentTriggerButton
            label="Agent Fix"
            size="xs"
            pending={agentFixPending}
            disabled={agentFixDisabled || busy}
            title={agentFixTitle ?? 'Diagnose and fix readiness signals via Cluster · Remediate'}
            onClick={onAgentFix}
          />
        )}
        {beat === 'settled' &&
          (settledOutcome === 'signal-unchanged' || settledOutcome === 'timeout') &&
          settleNavigate != null && (
            <Button
              type="button"
              variant="outline"
              size="xs"
              className="h-6 gap-1 text-[10px]"
              onClick={() => handleNavigate(settleNavigate)}
            >
              <ExternalLink size={10} aria-hidden />
              Inspect
            </Button>
          )}
      </div>
      {statusMsg != null && (
        <p className={cn('m-0 text-muted-foreground', dense ? 'mt-0.5 text-[9px]' : 'mt-1 text-[var(--text-dense-caption)]')}>
          {beat === 'requesting' && (
            <Loader2 size={10} className="mr-1 inline animate-spin align-[-1px]" aria-hidden />
          )}
          {statusMsg}
        </p>
      )}
    </div>
  )
}

export type { ProdFixSignal }
