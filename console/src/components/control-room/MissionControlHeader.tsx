import {
  Bot,
  Radar,
  Rocket,
  Satellite,
  Server,
  Wrench,
  type LucideIcon,
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
  buildDiagnosticPrompt,
  missionStatus,
  missionStatusColor,
  signalColor,
  type MissionSnapshot,
  type ModuleState,
  type Signal,
} from '@/lib/control-room/missionSignals'
import type { OpenRuntimeMapFn } from '@/lib/runtime-map/runtimeMapNavigation'

interface MissionControlHeaderProps {
  snapshot: MissionSnapshot
  matrices: MatrixResponse[]
  context?: OpsContextResponse
  dataUpdatedAt: number
  onOpenRuntimeMap: OpenRuntimeMapFn
  onOpenCluster: () => void
  onOpenDelivery: () => void
  onOpenPlatformRelease: () => void
  onOpenAgentDesk: (opts?: { prefill: string }) => void
}

const ROCKET_MODULES: Array<{
  key: keyof Pick<MissionSnapshot, 'infra' | 'release' | 'control' | 'agent'>
  icon: LucideIcon
  name: string
  role: string
  onOpen: (p: MissionControlHeaderProps) => () => void
}> = [
  {
    key: 'infra',
    icon: Server,
    name: 'Infra',
    role: 'Runtime foundation — K3s cluster, nodes, workloads',
    onOpen: p => p.onOpenCluster,
  },
  {
    key: 'release',
    icon: Rocket,
    name: 'Release',
    role: 'Launch pipeline — CI/CD, deliver, STG smoke',
    onOpen: p => p.onOpenDelivery,
  },
  {
    key: 'control',
    icon: Radar,
    name: 'Control',
    role: 'Flight computer — platform-api, console, GitOps',
    onOpen: p => p.onOpenPlatformRelease,
  },
  {
    key: 'agent',
    icon: Bot,
    name: 'Agent',
    role: 'Autopilot — remediation runner, git bridge, drift repair',
    onOpen: p => p.onOpenAgentDesk,
  },
]

function countReach(matrix: MatrixResponse): { ok: number; fail: number; total: number } {
  let ok = 0
  let fail = 0
  for (const t of matrix.targets) {
    if (t.reachability === 'ok' || t.reachability === 'degraded') ok += 1
    else if (t.reachability === 'fail') fail += 1
  }
  return { ok, fail, total: matrix.targets.length }
}

function RocketModuleCard({
  icon: Icon,
  name,
  role,
  state,
  onClick,
}: {
  icon: LucideIcon
  name: string
  role: string
  state: ModuleState
  onClick: () => void
}) {
  return (
    <button type="button" className="mission-rocket-card" onClick={onClick} title={state.detail}>
      <Icon size={20} style={{ color: signalColor(state.signal) }} className="mission-rocket-card-icon" />
      <div className="mission-rocket-card-body">
        <div className="mission-rocket-card-name">{name}</div>
        <div className="mission-rocket-card-val">{state.value}</div>
        <div className="mission-rocket-card-role">{role}</div>
      </div>
    </button>
  )
}

function formatAge(epoch: number): string {
  const ms = Date.now() - epoch
  if (ms < 60_000) return 'just now'
  const min = Math.floor(ms / 60_000)
  if (min < 60) return `${min}m ago`
  return `${Math.floor(min / 60)}h ago`
}

export function MissionControlHeader(props: MissionControlHeaderProps) {
  const { snapshot, matrices, context, dataUpdatedAt, onOpenAgentDesk } = props
  const mission = missionStatus(snapshot.missionOverall)
  const rocketMission = missionStatus(snapshot.rocketOverall)
  const payloadMission = missionStatus(snapshot.payloadOverall)

  const diagnosticPrompt = buildDiagnosticPrompt(snapshot)

  return (
    <div className="mission-control flex w-full min-w-0 flex-col gap-4">
      {/* ── Mission status board ── */}
      <section className="mission-board">
        <div className="mission-board-status">
          <span className="mission-board-label">Mission status</span>
          <span className="mission-board-value" style={{ color: missionStatusColor(mission) }}>
            {mission}
          </span>
        </div>
        <div className="mission-board-divider" aria-hidden />
        <div className="mission-board-segment">
          <Rocket size={16} style={{ color: missionStatusColor(rocketMission) }} />
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

      {/* ── Rocket subsystems ── */}
      <OpsSection
        title="Rocket — Ops Platform subsystems"
        description="The launch vehicle that carries Trade. Each subsystem provides a layer of support for payload operations."
        bodyPadding="compact"
        overflow="visible"
      >
        <div className="mission-rocket-grid">
          {ROCKET_MODULES.map(mod => (
            <RocketModuleCard
              key={mod.key}
              icon={mod.icon}
              name={mod.name}
              role={mod.role}
              state={snapshot[mod.key]}
              onClick={mod.onOpen(props)}
            />
          ))}
        </div>
      </OpsSection>

      {/* ── Payload telemetry ── */}
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
