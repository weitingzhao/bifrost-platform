import { useCallback, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Button, cn } from '@bifrost/ui'
import { ExternalLink, Loader2, Wrench } from 'lucide-react'
import { postIbGatewayControl, rolloutRestartDeployment } from '@/api/platform'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import type { ProdFixSignal } from '@/lib/agent/prodEnvironmentFixPrompt'
import type { Signal } from '@/lib/control-room/missionSignals'
import {
  readinessChipFixActions,
  setSatelliteApiEnv,
  setSatelliteBusFocus,
  type ReadinessChipAction,
  type ReadinessChipContext,
} from '@/lib/task-mode/readinessChipActions'

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
  const failing = chips.filter(c => c.signal !== 'ok')
  const [lastMsg, setLastMsg] = useState<string | null>(null)

  const actuateM = useMutation({
    mutationFn: async (action: ReadinessChipAction) => {
      const resp = await runActuation(action)
      if (!resp.ok) throw new Error(resp.message || 'Actuation failed')
      return resp
    },
    onSuccess: resp => setLastMsg(resp.message ?? 'Actuation OK'),
    onError: (e: Error) => setLastMsg(e.message),
  })

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

  if (failing.length === 0) return null

  const primaryFailing = failing[0]
  const fixActions = readinessChipFixActions(primaryFailing.label, primaryFailing.signal, ctx)

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
          Fix · {primaryFailing.label}
        </span>
      </div>
      <p className={cn('m-0 text-muted-foreground', dense ? 'mt-0.5 text-[9px]' : 'mt-1 text-[var(--text-dense-caption)]')}>
        {primaryFailing.detail}
      </p>
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
              className="h-6 text-[10px]"
              disabled={disabled || actuateM.isPending}
              title={disabled ? 'Operator authentication required' : action.label}
              onClick={() => actuateM.mutate(action)}
            >
              {actuateM.isPending ? <Loader2 size={10} className="animate-spin" /> : null}
              {action.label}
            </Button>
          )
        })}
        {onAgentFix != null && (
          <AgentTriggerButton
            label="Agent Fix"
            size="xs"
            pending={agentFixPending}
            disabled={agentFixDisabled}
            title={agentFixTitle ?? 'Diagnose and fix readiness signals via Cluster · Remediate'}
            onClick={onAgentFix}
          />
        )}
      </div>
      {lastMsg != null && (
        <p className={cn('m-0 text-muted-foreground', dense ? 'mt-0.5 text-[9px]' : 'mt-1 text-[var(--text-dense-caption)]')}>
          {lastMsg}
        </p>
      )}
    </div>
  )
}

export type { ProdFixSignal }
