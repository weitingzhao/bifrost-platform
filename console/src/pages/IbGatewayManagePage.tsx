import { useCallback, useState } from 'react'
import { Button, ConfirmDialog, DenseTag } from '@bifrost/ui'
import { postIbGatewayControl } from '@/api/network'
import { IbGatewayCutoverStatusPanel } from '@/components/cluster/IbGatewayCutoverStatusPanel'
import { IbGatewayLiveStatusPanel } from '@/components/cluster/IbGatewayLiveStatusPanel'
import {
  compactIbGatewaySummary,
  ibGatewayExtraTags,
} from '@/components/cluster/ibGatewaySummaryModel'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
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
export function IbGatewayManagePage({ onNavigate }: { onNavigate?: (tabId: string) => void } = {}) {
  const liveProbe = useIbGatewayLiveProbe()
  const { canOperate } = usePlatformAuth()
  const [reconnectOpen, setReconnectOpen] = useState(false)
  const [acting, setActing] = useState(false)
  const [actionMsg, setActionMsg] = useState<string | null>(null)
  const [actionFailed, setActionFailed] = useState(false)

  const ibReach = liveProbe.isLoading ? 'unknown' : liveProbe.probeReach
  const verdict = reachToVerdict(ibReach)
  const extraTags = ibGatewayExtraTags(liveProbe.status)

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
