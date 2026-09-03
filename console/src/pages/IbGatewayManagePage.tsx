import { useCallback, useMemo, useState } from 'react'
import { Button, ConfirmDialog, DenseTag } from '@bifrost/ui'
import { postIbGatewayControl } from '@/api/network'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { IbGatewayCutoverStatusPanel } from '@/components/cluster/IbGatewayCutoverStatusPanel'
import { IbGatewayLiveStatusPanel } from '@/components/cluster/IbGatewayLiveStatusPanel'
import {
  analyzeIbGatewayProbe,
  buildIbGatewayAgentPack,
  buildIbGatewayDiagnosePrefill,
  gatherIbGatewayAgentSnapshot,
} from '@/components/cluster/ibGatewayAgentPack'
import {
  compactIbGatewaySummary,
  ibGatewayExtraTags,
} from '@/components/cluster/ibGatewaySummaryModel'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import type { OpenAgentDeskArg } from '@/lib/agent/openAgentDesk'
import {
  OpsVerdictStrip,
  type OpsVerdictLamp,
  type OpsVerdictTagVariant,
} from '@/components/layout/OpsVerdictStrip'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

function reachToVerdict(reach: 'ok' | 'degraded' | 'fail' | 'unknown'): {
  lamp: OpsVerdictLamp
  tagLabel: string
  tagVariant: OpsVerdictTagVariant
} {
  switch (reach) {
    case 'ok':
      return { lamp: 'ok', tagLabel: 'OK', tagVariant: 'success' }
    case 'degraded':
      return { lamp: 'degraded', tagLabel: 'DEGRADED', tagVariant: 'warning' }
    case 'fail':
      return { lamp: 'fail', tagLabel: 'FAIL', tagVariant: 'danger' }
    default:
      return { lamp: 'unknown', tagLabel: 'UNKNOWN', tagVariant: 'neutral' }
  }
}

/**
 * Subcontractors → IB Gateway — observe live probe + Trade cutover (≠ Launch Plugin publish).
 */
type CopyState = 'idle' | 'busy' | 'copied' | 'error'

async function writeClipboard(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text)
    return
  } catch {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.style.position = 'fixed'
    ta.style.left = '-9999px'
    document.body.appendChild(ta)
    ta.focus()
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    if (!ok) throw new Error('clipboard write failed')
  }
}

export function IbGatewayManagePage({
  onNavigate,
  onOpenAgentDesk,
}: {
  onNavigate?: (tabId: string) => void
  onOpenAgentDesk?: (arg: OpenAgentDeskArg) => void
} = {}) {
  const liveProbe = useIbGatewayLiveProbe()
  const { canOperate } = usePlatformAuth()
  const [reconnectOpen, setReconnectOpen] = useState(false)
  const [acting, setActing] = useState(false)
  const [actionMsg, setActionMsg] = useState<string | null>(null)
  const [actionFailed, setActionFailed] = useState(false)
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const ibReach = liveProbe.isLoading ? 'unknown' : liveProbe.probeReach
  const verdict = reachToVerdict(ibReach)
  const extraTags = ibGatewayExtraTags(liveProbe.status)

  const diagnosePrefill = useMemo(() => {
    const analysis = analyzeIbGatewayProbe(liveProbe.status)
    return buildIbGatewayDiagnosePrefill({
      generatedAt: new Date().toISOString(),
      status: liveProbe.status ?? null,
      statusError: liveProbe.status?.error ?? null,
      selfHeal: null,
      selfHealError: null,
      analysis,
    })
  }, [liveProbe.status])

  const handleCopyForAgent = useCallback(async () => {
    setCopyState('busy')
    try {
      const snap = await gatherIbGatewayAgentSnapshot()
      await writeClipboard(buildIbGatewayAgentPack(snap))
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2500)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [])

  const runReconnect = useCallback(async () => {
    setActing(true)
    setActionMsg(null)
    setActionFailed(false)
    try {
      const resp = await postIbGatewayControl('reconnect')
      setActionFailed(!resp.ok)
      setActionMsg(resp.ok ? resp.message : `Failed: ${resp.message}`)
      if (resp.ok) liveProbe.refetch()
    } catch (e) {
      setActionFailed(true)
      setActionMsg(e instanceof Error ? e.message : 'Reconnect failed')
    } finally {
      setActing(false)
      setReconnectOpen(false)
    }
  }, [liveProbe])

  return (
    <div className="flex w-full min-w-0 flex-col gap-2">
      <OpsVerdictStrip
        compact
        ariaLabel="IB Gateway plugin verdict"
        title="IB GATEWAY"
        lamp={verdict.lamp}
        tagLabel={verdict.tagLabel}
        tagVariant={verdict.tagVariant}
        summary={
          liveProbe.isLoading
            ? 'Probing ib-gateway…'
            : compactIbGatewaySummary(liveProbe.status)
        }
        extraTags={
          <>
            {extraTags.map(t => (
              <DenseTag key={t.label} variant={t.variant}>
                {t.label}
              </DenseTag>
            ))}
          </>
        }
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              disabled={copyState === 'busy'}
              title="Copy a repair pack (status + self-heal + ghost-session checks) for an AI agent"
              onClick={() => void handleCopyForAgent()}
            >
              {copyState === 'busy'
                ? 'Exporting…'
                : copyState === 'copied'
                  ? 'Copied!'
                  : copyState === 'error'
                    ? 'Copy failed'
                    : 'Copy for Agent'}
            </Button>
            {onOpenAgentDesk != null ? (
              <AgentTriggerButton
                label="Diagnose with Agent"
                size="sm"
                title="Open Agent Desk with IB Client diagnose prefill (read-first plan)"
                onClick={() => onOpenAgentDesk({ prefill: diagnosePrefill })}
              />
            ) : null}
            <Button
              variant="outline"
              size="sm"
              disabled={liveProbe.isLoading}
              onClick={() => liveProbe.refetch()}
            >
              Refresh
            </Button>
            {canOperate ? (
              <Button
                variant="outline"
                size="sm"
                disabled={acting}
                onClick={() => setReconnectOpen(true)}
              >
                Reconnect
              </Button>
            ) : null}
            {onNavigate != null ? (
              <Button variant="outline" size="sm" onClick={() => onNavigate('plugin-release')}>
                Need publish?
              </Button>
            ) : null}
          </>
        }
        meta={
          <>
            {liveProbe.status?.mode != null && liveProbe.status.mode !== '' ? (
              <span>mode {liveProbe.status.mode}</span>
            ) : null}
            {liveProbe.status?.deployment?.ready != null &&
            liveProbe.status.deployment.ready !== '' ? (
              <span>deployment {liveProbe.status.deployment.ready}</span>
            ) : null}
          </>
        }
      />

      {actionMsg != null ? (
        <OpsFeedback variant={actionFailed ? 'error' : 'success'} title="Reconnect">
          {actionMsg}
        </OpsFeedback>
      ) : null}

      <IbGatewayLiveStatusPanel showPrimaryActions={false} />
      <IbGatewayCutoverStatusPanel />

      <ConfirmDialog
        open={reconnectOpen}
        title="Reconnect IB Gateway"
        message="Rollout restart deployment/ib-gateway in data NS. Use when TWS sessions need a clean reconnect."
        confirmLabel="Confirm reconnect"
        confirming={acting}
        onConfirm={() => void runReconnect()}
        onCancel={() => setReconnectOpen(false)}
      />
    </div>
  )
}
