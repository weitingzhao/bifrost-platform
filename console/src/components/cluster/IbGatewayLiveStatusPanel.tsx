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
import type { IbGatewaySlotStatus } from '@/api/satelliteBusTypes'
import { postIbGatewayControl } from '@/api/network'
import {
  DashCard,
  Meter,
  ScoreRing,
} from '@/components/market-data/overviewDash'
import { toneByLevel } from '@/components/market-data/overviewDashModel'
import { OpsSection } from '@/components/layout/OpsSection'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

function reachTagVariant(reach: string): 'success' | 'warning' | 'danger' | 'neutral' {
  if (reach === 'ok') return 'success'
  if (reach === 'degraded') return 'warning'
  if (reach === 'fail') return 'danger'
  return 'neutral'
}

function slotTone(slot: IbGatewaySlotStatus): 'ok' | 'scheduled' | 'missing' | 'unknown' {
  if (slot.connected && slot.reachability === 'ok') return 'ok'
  if (slot.connected || slot.reachability === 'degraded') return 'scheduled'
  if (slot.reachability === 'fail') return 'missing'
  return 'unknown'
}

function SlotCard({
  slot,
  canOperate,
  acting,
  onMaintenance,
}: {
  slot: IbGatewaySlotStatus
  canOperate: boolean
  acting: boolean
  onMaintenance: (enabled: boolean, accountId: string) => void
}) {
  const tone = slotTone(slot)
  return (
    <DashCard
      title={slot.slot}
      tag={slot.status}
      tagVariant={slot.connected ? 'success' : 'neutral'}
      value={slot.connected ? 'connected' : 'down'}
      caption={`${slot.account_id} · reach ${slot.reachability}`}
      captionTitle={slot.detail}
    >
      <Meter fillPct={slot.connected ? 100 : 0} toneClass={toneByLevel(tone)} label={slot.slot} />
      {canOperate ? (
        <div className="mt-1 flex gap-1">
          <Button
            variant="ghost"
            size="xs"
            disabled={acting}
            onClick={() => onMaintenance(true, slot.account_id)}
          >
            Enter
          </Button>
          <Button
            variant="ghost"
            size="xs"
            disabled={acting}
            onClick={() => onMaintenance(false, slot.account_id)}
          >
            Clear
          </Button>
        </div>
      ) : null}
    </DashCard>
  )
}

export function IbGatewayLiveStatusPanel({
  showPrimaryActions = true,
  embedded = false,
}: {
  showPrimaryActions?: boolean
  embedded?: boolean
} = {}) {
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

  const runModeSwitch = useCallback(
    async (mode: 'live' | 'mock') => {
      setActing(true)
      setActionMsg(null)
      try {
        const resp = await postIbGatewayControl('mode', { mode })
        setActionMsg(resp.ok ? resp.message : `Failed: ${resp.message}`)
        if (resp.ok) void liveProbe.refetch()
      } catch (e) {
        setActionMsg(e instanceof Error ? e.message : 'Mode switch failed')
      } finally {
        setActing(false)
        setModeConfirm(null)
      }
    },
    [liveProbe],
  )

  const status = liveProbe.status
  const currentMode = status?.mode?.toLowerCase()
  const slots = status?.slots ?? []
  const connected = slots.filter(s => s.connected).length
  const degraded = slots.filter(s => !s.connected && s.reachability !== 'fail').length
  const failed = slots.length - connected - degraded

  const modeButtons =
    currentMode === 'mock' ? (
      <Button variant="default" size="xs" disabled={acting} onClick={() => setModeConfirm('live')}>
        Switch to live
      </Button>
    ) : currentMode === 'live' ? (
      <Button variant="outline" size="xs" disabled={acting} onClick={() => setModeConfirm('mock')}>
        Revert to mock
      </Button>
    ) : null

  const reconnectButton = showPrimaryActions ? (
    <Button variant="outline" size="xs" disabled={acting} onClick={() => setReconnectOpen(true)}>
      Reconnect
    </Button>
  ) : null

  const sectionActions =
    canOperate && (modeButtons != null || reconnectButton != null) ? (
      <div className="flex flex-wrap gap-2">
        {modeButtons}
        {reconnectButton}
      </div>
    ) : undefined

  return (
    <OpsSection
      variant={embedded ? 'flat' : 'elevated'}
      title="IB Gateway live"
      description="TWS slots · mode switch here"
      actions={sectionActions}
      headerExtra={
        <DenseTag variant={reachTagVariant(liveProbe.probeReach)}>
          {liveProbe.isLoading ? '…' : liveProbe.probeReach}
        </DenseTag>
      }
      bodyPadding="compact"
      overflow="visible"
      collapsible={!embedded}
      defaultCollapsed={false}
    >
      <div className="flex flex-col gap-1.5">
        <div className="flex items-stretch gap-2">
          <ScoreRing
            ready={connected}
            thin={degraded}
            blocked={failed}
            total={Math.max(slots.length, 1)}
            caption="conn"
          />
          <div className="grid min-w-0 flex-1 grid-cols-1 gap-1.5 sm:grid-cols-2">
            {slots.map(slot => (
              <SlotCard
                key={slot.slot}
                slot={slot}
                canOperate={canOperate}
                acting={acting}
                onMaintenance={(en, id) => void runMaintenance(en, id)}
              />
            ))}
          </div>
        </div>

        {status?.hint != null && status.reachable !== true ? (
          <p
            className="m-0 line-clamp-2 break-words text-[var(--text-dense-caption)] text-warning"
            title={status.hint}
          >
            {status.hint}
          </p>
        ) : null}

        {actionMsg != null ? (
          <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            {actionMsg}
          </p>
        ) : null}

        <OpsSection
          variant="flat"
          title="Slot table"
          collapsible
          defaultCollapsed
          bodyPadding="none"
          overflow="visible"
        >
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Slot</DenseTableHead>
                <DenseTableHead>Account</DenseTableHead>
                <DenseTableHead>Status</DenseTableHead>
                <DenseTableHead>Reach</DenseTableHead>
                {canOperate ? <DenseTableHead>Maintenance</DenseTableHead> : null}
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {slots.map(slot => (
                <DenseTableRow key={slot.slot}>
                  <DenseTableCell className="font-mono text-xs">{slot.slot}</DenseTableCell>
                  <DenseTableCell>{slot.account_id}</DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={slot.connected ? 'success' : 'neutral'}>
                      {slot.status}
                    </DenseTag>
                  </DenseTableCell>
                  <DenseTableCell>
                    <StatusLamp value={slot.reachability} kind="reach" />
                  </DenseTableCell>
                  {canOperate ? (
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
                  ) : null}
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </OpsSection>
      </div>

      <ConfirmDialog
        open={showPrimaryActions && reconnectOpen}
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
