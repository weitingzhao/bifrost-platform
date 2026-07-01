import {
  Satellite,
  Wrench,
} from 'lucide-react'
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
import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  buildControlRoomDispatchPack,
} from '@/lib/control-room/controlRoomOperatePack'
import {
  missionStatus,
  missionStatusColor,
  signalColor,
  type MissionSnapshot,
  type Signal,
} from '@/lib/control-room/missionSignals'
import type { OpenRuntimeMapFn } from '@/lib/runtime-map/runtimeMapNavigation'
import { PayloadDepthPanel } from '@/components/control-room/PayloadDepthPanel'
import { RocketSubsystemsGrid } from '@/components/control-room/RocketSubsystemsGrid'

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
  onOpenProgram?: () => void
  onOpenPlatformRelease: () => void
  onOpenAgentDesk: (opts?: { prefill: string }) => void
}

function countReach(matrix: MatrixResponse): { ok: number; fail: number; total: number } {
  let ok = 0
  let fail = 0
  for (const t of matrix.targets) {
    if (t.reachability === 'ok' || t.reachability === 'degraded') ok += 1
    else if (t.reachability === 'fail') fail += 1
  }
  return { ok, fail, total: matrix.targets.length }
}

function formatAge(epoch: number): string {
  const ms = Date.now() - epoch
  if (ms < 60_000) return 'just now'
  const min = Math.floor(ms / 60_000)
  if (min < 60) return `${min}m ago`
  return `${Math.floor(min / 60)}h ago`
}

export function MissionControlHeader(props: MissionControlHeaderProps) {
  const {
    snapshot,
    matrices,
    context,
    dataUpdatedAt,
    showRocketSubsystems = true,
    onOpenAgentDesk,
  } = props
  const mission = missionStatus(snapshot.missionOverall)
  const rocketMission = missionStatus(snapshot.rocketOverall)
  const payloadMission = missionStatus(snapshot.payloadOverall)

  const diagnosticPrompt = buildControlRoomDispatchPack({
    snapshot,
    matrices,
    context,
  })

  return (
    <div className="mission-control flex w-full min-w-0 flex-col gap-4">
      <section className="mission-board">
        <div className="mission-board-status">
          <span className="mission-board-label">Mission status</span>
          <span className="mission-board-value" style={{ color: missionStatusColor(mission) }}>
            {mission}
          </span>
        </div>
        <div className="mission-board-divider" aria-hidden />
        <div className="mission-board-segment">
          <span className="mission-board-seg-label">Rocket</span>
          <span className="mission-board-seg-val" style={{ color: missionStatusColor(rocketMission) }}>
            {rocketMission}
          </span>
        </div>
        <div className="mission-board-segment">
          <Satellite size={16} style={{ color: missionStatusColor(payloadMission) }} />
          <span className="mission-board-seg-label">Payload</span>
          <span className="mission-board-seg-val" style={{ color: missionStatusColor(payloadMission) }}>
            {payloadMission}
          </span>
        </div>
        {diagnosticPrompt != null && (
          <>
            <div className="mission-board-divider" aria-hidden />
            <button
              type="button"
              className="mission-board-fix"
              onClick={() => onOpenAgentDesk({ prefill: diagnosticPrompt })}
              title="Open Agent Desk with a pre-filled diagnostic prompt based on current failures"
            >
              <Wrench size={14} />
              <span>Diagnose &amp; Fix</span>
            </button>
          </>
        )}
        {context?.focus.headline != null && context.focus.headline !== '' && (
          <>
            <div className="mission-board-divider" aria-hidden />
            <div className="mission-board-focus">
              <span className="mission-board-focus-label">Focus</span>
              <span className="mission-board-focus-text">{context.focus.headline.split('—')[0]?.trim()}</span>
            </div>
          </>
        )}
        <div className="mission-board-ts">
          {dataUpdatedAt > 0 ? formatAge(dataUpdatedAt) : 'probing…'}
        </div>
      </section>

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
            onOpenDelivery={props.onOpenDelivery}
            onOpenProgram={props.onOpenProgram}
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
