import { useCallback, useState } from 'react'
import { Button, ConfirmDialog, DenseTag } from '@bifrost/ui'
import { postIbGatewayControl } from '@/api/network'
import { IbGatewayCutoverStatusPanel } from '@/components/cluster/IbGatewayCutoverStatusPanel'
import { IbGatewayLiveStatusPanel } from '@/components/cluster/IbGatewayLiveStatusPanel'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  OpsVerdictStrip,
  type OpsVerdictLamp,
  type OpsVerdictTagVariant,
} from '@/components/layout/OpsVerdictStrip'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

const PLUGIN_REGISTRY = [
  {
    id: 'ib-gateway',
    name: 'IB Gateway',
    vendor: 'Interactive Brokers',
    role: 'TWS socket bridge · redis-ib @ data NS',
    status: 'live',
  },
  {
    id: 'massive-stock',
    name: 'Massive Stock feed',
    vendor: 'Polygon.io',
    role: 'Planned subcontractor plugin',
    status: 'planned',
  },
  {
    id: 'massive-option',
    name: 'Massive Option feed',
    vendor: 'Polygon.io',
    role: 'Planned subcontractor plugin',
    status: 'planned',
  },
  {
    id: 'flex-query',
    name: 'IB Flex Query',
    vendor: 'Interactive Brokers',
    role: 'Planned subcontractor plugin',
    status: 'planned',
  },
] as const

function statusVariant(status: string): 'success' | 'neutral' | 'info' {
  if (status === 'live') return 'success'
  if (status === 'planned') return 'neutral'
  return 'info'
}

function pluginBusVerdict(liveProbe: ReturnType<typeof useIbGatewayLiveProbe>): {
  lamp: OpsVerdictLamp
  tagLabel: string
  tagVariant: OpsVerdictTagVariant
  summary: string
} {
  if (liveProbe.isLoading) {
    return {
      lamp: 'unknown',
      tagLabel: 'PROBING',
      tagVariant: 'neutral',
      summary: 'Probing ib-gateway via platform-api…',
    }
  }

  const mode = liveProbe.status?.mode
  const modeNote = mode != null && mode !== '' ? ` · mode ${mode}` : ''
  const deploy = liveProbe.status?.deployment?.ready
  const deployNote = deploy != null && deploy !== '' ? ` · deployment ${deploy}` : ''

  switch (liveProbe.probeReach) {
    case 'ok':
      return {
        lamp: 'ok',
        tagLabel: 'OK',
        tagVariant: 'success',
        summary: `${liveProbe.summary}${modeNote}${deployNote}`,
      }
    case 'degraded':
      return {
        lamp: 'degraded',
        tagLabel: 'DEGRADED',
        tagVariant: 'warning',
        summary: `${liveProbe.summary}${modeNote}${deployNote}`,
      }
    case 'fail':
      return {
        lamp: 'fail',
        tagLabel: 'FAIL',
        tagVariant: 'danger',
        summary: `${liveProbe.summary}${modeNote}${deployNote}`,
      }
    default:
      return {
        lamp: 'unknown',
        tagLabel: 'UNKNOWN',
        tagVariant: 'neutral',
        summary: `${liveProbe.summary}${modeNote}${deployNote}`,
      }
  }
}

export function PluginGalleryPage() {
  const liveProbe = useIbGatewayLiveProbe()
  const { canOperate } = usePlatformAuth()
  const [reconnectOpen, setReconnectOpen] = useState(false)
  const [acting, setActing] = useState(false)
  const [actionMsg, setActionMsg] = useState<string | null>(null)
  const [actionFailed, setActionFailed] = useState(false)

  const liveCount = PLUGIN_REGISTRY.filter(p => p.status === 'live').length
  const plannedCount = PLUGIN_REGISTRY.filter(p => p.status === 'planned').length
  const verdict = pluginBusVerdict(liveProbe)

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
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsVerdictStrip
        ariaLabel="Plugin bus verdict"
        title="PLUGIN BUS · IB GATEWAY"
        lamp={verdict.lamp}
        tagLabel={verdict.tagLabel}
        tagVariant={verdict.tagVariant}
        summary={verdict.summary}
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
          </>
        }
        meta={
          <span>
            {liveCount} live · {plannedCount} planned
            {liveProbe.status?.mode != null && liveProbe.status.mode !== ''
              ? ` · mode ${liveProbe.status.mode}`
              : ''}
          </span>
        }
      />

      {actionMsg != null ? (
        <OpsFeedback variant={actionFailed ? 'error' : 'success'} title="Reconnect">
          {actionMsg}
        </OpsFeedback>
      ) : null}

      <OpsSection title="Plugin registry" bodyPadding="default" overflow="visible">
        <div className="grid gap-2 sm:grid-cols-2">
          {PLUGIN_REGISTRY.map(plugin => (
            <div
              key={plugin.id}
              className="rounded-md border border-[var(--border)] bg-[var(--secondary)] px-3 py-2"
            >
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-[var(--text-dense-label)] font-semibold">{plugin.name}</span>
                <DenseTag variant={statusVariant(plugin.status)}>{plugin.status}</DenseTag>
              </div>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                {plugin.vendor} · {plugin.role}
              </p>
            </div>
          ))}
        </div>
      </OpsSection>

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
