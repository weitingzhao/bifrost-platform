import { useCallback, useState } from 'react'
import {
  Button,
  ConfirmDialog,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  StatusLamp,
} from '@bifrost/ui'
import { postIbGatewayControl } from '@/api/network'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { OpsSection } from '@/components/layout/OpsSection'

function reachTagVariant(reach: string): 'success' | 'warning' | 'danger' | 'neutral' {
  if (reach === 'ok') return 'success'
  if (reach === 'degraded') return 'warning'
  if (reach === 'fail') return 'danger'
  return 'neutral'
}

export function IbGatewayLiveStatusPanel() {
  const liveProbe = useIbGatewayLiveProbe()
  const { canOperate } = usePlatformAuth()
  const [reconnectOpen, setReconnectOpen] = useState(false)
  const [modeConfirm, setModeConfirm] = useState<'live' | 'mock' | null>(null)
  const [acting, setActing] = useState(false)
  const [actionMsg, setActionMsg] = useState<string | null>(null)

  const runReconnect = useCallback(async () => {
    setActing(true)
    setActionMsg(null)
    try {
      const resp = await postIbGatewayControl('reconnect')
      setActionMsg(resp.ok ? resp.message : `Failed: ${resp.message}`)
    } catch (e) {
      setActionMsg(e instanceof Error ? e.message : 'Reconnect failed')
    } finally {
      setActing(false)
      setReconnectOpen(false)
    }
  }, [])

  const runMaintenance = useCallback(async (enabled: boolean, accountId: string) => {
    setActing(true)
    setActionMsg(null)
    try {
      const resp = await postIbGatewayControl('maintenance', { account_id: accountId, enabled })
      setActionMsg(resp.ok ? resp.message : `Failed: ${resp.message}`)
    } catch (e) {
      setActionMsg(e instanceof Error ? e.message : 'Maintenance action failed')
    } finally {
      setActing(false)
    }
  }, [])

  const runModeSwitch = useCallback(async (mode: 'live' | 'mock') => {
    setActing(true)
    setActionMsg(null)
    try {
      const resp = await postIbGatewayControl('mode', { mode })
      setActionMsg(resp.ok ? resp.message : `Failed: ${resp.message}`)
      if (resp.ok) {
        void liveProbe.refetch()
      }
    } catch (e) {
      setActionMsg(e instanceof Error ? e.message : 'Mode switch failed')
    } finally {
      setActing(false)
      setModeConfirm(null)
    }
  }, [liveProbe])

  const status = liveProbe.status
  const currentMode = status?.mode?.toLowerCase()

  return (
    <OpsSection
      title="IB Gateway live status"
      description="L0 probe via GET /api/v1/plugins/ib-gateway/status — redis-ib health + K8s deployment @ data NS."
      actions={
        canOperate ? (
          <div className="flex flex-wrap gap-2">
            {currentMode === 'mock' && (
              <Button variant="default" size="xs" disabled={acting} onClick={() => setModeConfirm('live')}>
                Switch to live
              </Button>
            )}
            {currentMode === 'live' && (
              <Button variant="outline" size="xs" disabled={acting} onClick={() => setModeConfirm('mock')}>
                Revert to mock
              </Button>
            )}
            <Button variant="outline" size="xs" disabled={acting} onClick={() => setReconnectOpen(true)}>
              Reconnect (rollout restart)
            </Button>
          </div>
        ) : undefined
      }
      bodyPadding="default"
    >
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <StatusLamp value={liveProbe.probeReach} kind="reach" />
        <DenseTag variant={reachTagVariant(liveProbe.probeReach)}>
          {liveProbe.isLoading ? 'PROBING…' : liveProbe.probeReach.toUpperCase()}
        </DenseTag>
        <DenseTag variant="info">L0 status</DenseTag>
        {status?.mode != null && (
          <DenseTag variant={currentMode === 'live' ? 'warning' : 'neutral'}>
            mode: {status.mode}
          </DenseTag>
        )}
        {status?.deployment?.ready != null && (
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            deployment {status.deployment.ready}
          </span>
        )}
      </div>

      <p className="m-0 mb-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        {liveProbe.summary}
      </p>

      {status?.hint != null && status.reachable !== true && (
        <p className="m-0 mb-3 text-[var(--text-dense-caption)] text-[var(--warning)]">{status.hint}</p>
      )}

      {actionMsg != null && (
        <p className="m-0 mb-3 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">{actionMsg}</p>
      )}

      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Slot</DenseTableHead>
            <DenseTableHead>Account</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Reach</DenseTableHead>
            {canOperate && <DenseTableHead>Maintenance</DenseTableHead>}
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {(status?.slots ?? []).map(slot => (
            <DenseTableRow key={slot.slot}>
              <DenseTableCell className="font-mono text-xs">{slot.slot}</DenseTableCell>
              <DenseTableCell>{slot.account_id}</DenseTableCell>
              <DenseTableCell>
                <DenseTag variant={slot.connected ? 'success' : 'neutral'}>{slot.status}</DenseTag>
              </DenseTableCell>
              <DenseTableCell>
                <StatusLamp value={slot.reachability} kind="reach" />
              </DenseTableCell>
              {canOperate && (
                <DenseTableCell>
                  <div className="flex gap-1">
                    <Button
                      variant="ghost"
                      size="xs"
                      disabled={acting}
                      onClick={() => void runMaintenance(true, slot.account_id)}
                    >
                      Enter
                    </Button>
                    <Button
                      variant="ghost"
                      size="xs"
                      disabled={acting}
                      onClick={() => void runMaintenance(false, slot.account_id)}
                    >
                      Clear
                    </Button>
                  </div>
                </DenseTableCell>
              )}
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>

      <ConfirmDialog
        open={reconnectOpen}
        title="Reconnect IB Gateway"
        message="Rollout restart deployment/ib-gateway in data NS. Use when TWS sessions need a clean reconnect."
        confirmLabel="Confirm reconnect"
        confirming={acting}
        onConfirm={() => void runReconnect()}
        onCancel={() => setReconnectOpen(false)}
      />

      <ConfirmDialog
        open={modeConfirm === 'live'}
        title="Switch IB Gateway to live"
        message="Patch ConfigMap mode=live and rollout restart. ib-gateway will connect to real TWS @ Host (.30) and Secondary (.32). Ensure TWS is running and market access is intended."
        confirmLabel="Confirm live"
        confirming={acting}
        onConfirm={() => void runModeSwitch('live')}
        onCancel={() => setModeConfirm(null)}
      />

      <ConfirmDialog
        open={modeConfirm === 'mock'}
        title="Revert IB Gateway to mock"
        message="Patch ConfigMap mode=mock and rollout restart. Live TWS sockets will disconnect; mock tick/account data resumes."
        confirmLabel="Confirm mock"
        confirming={acting}
        onConfirm={() => void runModeSwitch('mock')}
        onCancel={() => setModeConfirm(null)}
      />
    </OpsSection>
  )
}
