import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  Button,
} from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import { fetchVerifyPayload } from '@/api/platform'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  buildControlRoomDispatchPack,
} from '@/lib/control-room/controlRoomOperatePack'
import {
  signalColor,
  type MissionSnapshot,
  type Signal,
} from '@/lib/control-room/missionSignals'
import { countsTowardTradeReadiness } from '@/lib/control-room/matrixSummary'
import type { OpenRuntimeMapFn } from '@/lib/runtime-map/runtimeMapNavigation'
import { PayloadDepthPanel } from '@/components/control-room/PayloadDepthPanel'
import { RocketSubsystemsGrid } from '@/components/control-room/RocketSubsystemsGrid'
import { MissionBoard } from '@/components/control-room/MissionBoard'

interface MissionControlHeaderProps {
  snapshot: MissionSnapshot
  matrices: MatrixResponse[]
  context?: OpsContextResponse
  dataUpdatedAt: number
  /** When false, rocket subsystem cards are omitted (shown under Program context). */
  showRocketSubsystems?: boolean
  onOpenRuntimeMap: OpenRuntimeMapFn
  onOpenCluster: () => void
  onOpenDelivery: () => void
  onOpenPlatformRelease: () => void
  onOpenAgentDesk: (opts?: { prefill: string }) => void
  onOpenLaunchView: (mode: 'mission-launch') => void
  /** Trade readiness IB → Daily Ops Fleet Vendor */
  onOpenFleetVendor?: () => void
  onOpenPromote?: () => void
  onPlaybookFix?: (opts: { scope: string; prompt: string }) => void
  playbookFixPending?: boolean
  canOperate?: boolean
}

function countReach(matrix: MatrixResponse): { ok: number; fail: number; total: number } {
  let ok = 0
  let fail = 0
  let total = 0
  for (const t of matrix.targets) {
    if (!countsTowardTradeReadiness(t)) continue
    total += 1
    if (t.reachability === 'ok' || t.reachability === 'degraded') ok += 1
    else if (t.reachability === 'fail') fail += 1
  }
  return { ok, fail, total }
}


export function MissionControlHeader(props: MissionControlHeaderProps) {
  const {
    snapshot,
    matrices,
    context,
    dataUpdatedAt,
    showRocketSubsystems = true,
    onOpenAgentDesk,
    onOpenRuntimeMap,
    onOpenLaunchView,
  } = props

  const verifyQ = useQuery({
    queryKey: ['cockpit', 'verify-payload'],
    queryFn: fetchVerifyPayload,
    refetchInterval: 20_000,
  })

  const diagnosticPrompt = buildControlRoomDispatchPack({
    snapshot,
    matrices,
    context,
    verify: verifyQ.data,
  })

  return (
    <div className="mission-control flex w-full min-w-0 flex-col gap-4">
      <MissionBoard
        snapshot={snapshot}
        matrices={matrices}
        context={context}
        dataUpdatedAt={dataUpdatedAt}
        diagnosticPrompt={diagnosticPrompt}
        onOpenLaunchView={onOpenLaunchView}
        onOpenAgentDesk={onOpenAgentDesk}
        onOpenRuntimeMap={onOpenRuntimeMap}
        onPlaybookFix={props.onPlaybookFix}
        playbookFixPending={props.playbookFixPending}
        canOperate={props.canOperate}
      />

      {context?.focus.blocker != null && context.focus.blocker !== '' && (
        <section className="mission-blocker">
          <strong>Mission blocker:</strong> {context.focus.blocker}
        </section>
      )}

      {showRocketSubsystems && (
        <OpsSection
          title="Rocket — Ops Platform subsystems"
          description="The launch vehicle that carries Trade. Each subsystem provides a layer of support for payload operations."
          bodyPadding="compact"
          overflow="visible"
        >
          <RocketSubsystemsGrid
            snapshot={snapshot}
            onOpenCluster={props.onOpenCluster}
            onOpenDelivery={props.onOpenDelivery}
            onOpenPlatformRelease={props.onOpenPlatformRelease}
            onOpenAgentDesk={() => onOpenAgentDesk()}
          />
        </OpsSection>
      )}

      <OpsSection
        title="Payload — Trade satellite"
        description="Business stack reachability. Ops Platform exists to keep this payload stable, released, and maintained."
        actions={
          <Button variant="ghost" size="xs" onClick={() => props.onOpenRuntimeMap()}>
            Open Runtime Map
          </Button>
        }
        bodyPadding="none"
        overflow="hidden"
      >
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Environment</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
              <DenseTableHead>Reachable</DenseTableHead>
              <DenseTableHead>Fail</DenseTableHead>
              <DenseTableHead>Total</DenseTableHead>
              <DenseTableHead>Probed</DenseTableHead>
              <DenseTableHead />
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {matrices.map(m => {
              const c = countReach(m)
              const envState = m.environment === 'dev' ? snapshot.tradeDev : snapshot.tradeProd
              return (
                <DenseTableRow key={m.environment}>
                  <DenseTableCell>
                    <button
                      type="button"
                      className="mission-env-link"
                      onClick={() => props.onOpenRuntimeMap({ env: m.environment })}
                    >
                      <span className={`badge-ui badge-env-${m.environment}`}>{m.environment}</span>
                    </button>
                  </DenseTableCell>
                  <DenseTableCell>
                    <EnvStatusBadge signal={envState.signal} />
                  </DenseTableCell>
                  <DenseTableCell className="font-mono-tabular">{envState.value}</DenseTableCell>
                  <DenseTableCell className={`font-mono-tabular ${c.fail > 0 ? 'lamp-fail' : ''}`}>
                    {c.fail}
                  </DenseTableCell>
                  <DenseTableCell className="font-mono-tabular">{c.total}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                    {m.generated_at}
                  </DenseTableCell>
                  <DenseTableCell>
                    <Button
                      variant="ghost"
                      size="xs"
                      onClick={() => props.onOpenRuntimeMap({ env: m.environment })}
                    >
                      {c.fail > 0 ? 'Drill down' : 'Open map'}
                    </Button>
                  </DenseTableCell>
                </DenseTableRow>
              )
            })}
            {matrices.length === 0 && (
              <DenseTableRow>
                <DenseTableCell colSpan={7} className="text-[var(--muted-foreground)]">
                  Probing Trade matrix…
                </DenseTableCell>
              </DenseTableRow>
            )}
          </DenseTableBody>
        </DenseDataTable>

        <div className="payload-depth-inset">
          <PayloadDepthPanel
            matrices={matrices}
            context={context}
            onOpenRuntimeMap={props.onOpenRuntimeMap}
            onOpenFleetVendor={props.onOpenFleetVendor}
          />
        </div>
      </OpsSection>
    </div>
  )
}

function EnvStatusBadge({ signal }: { signal: Signal }) {
  const label = signal === 'ok' ? 'NOMINAL' : signal === 'degraded' ? 'CAUTION' : signal === 'fail' ? 'CRITICAL' : 'PROBING'
  return (
    <span className="mission-env-badge" style={{ color: signalColor(signal) }}>
      {label}
    </span>
  )
}
