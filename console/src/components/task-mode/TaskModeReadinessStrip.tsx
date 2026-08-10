import { DenseTag, StatusLamp } from '@bifrost/ui'
import { Gauge } from 'lucide-react'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import { missionStatus, missionStatusColor } from '@/lib/control-room/missionSignals'
import type { TaskModeId } from '@/lib/task-mode/types'
import { ReadinessChip, RocketReadinessStrip } from '@/components/task-mode/readiness/RocketReadinessStrip'
import { SatelliteReadinessStrip } from '@/components/task-mode/readiness/SatelliteReadinessStrip'

export type TaskModeReadinessStripProps = {
  modeId: TaskModeId
  onNavigate: (tabId: string) => void
  compact?: boolean
  summaryColumn?: boolean
  suppressProdBlockedBanner?: boolean
  canOperate?: boolean
  onAgentFixStg?: () => void
  onAgentFixProd?: () => void
  agentFixPending?: boolean
  agentFixDisabled?: boolean
  agentFixTitle?: string
}

/** Mode-scoped environment readiness — replaces generic mission signals in playbook ops modes. */
export function TaskModeReadinessStrip({
  modeId,
  onNavigate,
  compact = false,
  summaryColumn = false,
  suppressProdBlockedBanner = false,
  canOperate = false,
  onAgentFixStg,
  onAgentFixProd,
  agentFixPending = false,
  agentFixDisabled = false,
  agentFixTitle,
}: TaskModeReadinessStripProps) {
  if (modeId === 'ops') {
    return (
      <div className={summaryColumn ? 'flex flex-col gap-1.5' : 'flex flex-col gap-3'}>
        <RocketReadinessStrip
          compact={compact}
          summaryColumn={summaryColumn}
          suppressProdBlockedBanner={suppressProdBlockedBanner}
          onNavigate={onNavigate}
        />
        <SatelliteReadinessStrip
          compact={compact}
          summaryColumn={summaryColumn}
          suppressProdBlockedBanner={suppressProdBlockedBanner}
          onNavigate={onNavigate}
          canOperate={canOperate}
          onAgentFixStg={onAgentFixStg}
          onAgentFixProd={onAgentFixProd}
          agentFixPending={agentFixPending}
          agentFixDisabled={agentFixDisabled}
          agentFixTitle={agentFixTitle}
        />
      </div>
    )
  }
  return null
}

/**
 * @deprecated Daily Ops main path uses DailyOpsFleetBoard (Fleet Desk).
 * Kept for secondary/legacy embeds only — do not wire as Task CC primary.
 */
export function DailyOpsMissionStrip({ compact = false }: { compact?: boolean }) {
  const { snapshot, isLoading } = useMissionSnapshot()
  const status = missionStatus(snapshot.missionOverall)
  const color = missionStatusColor(status)

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <Gauge size={16} style={{ color }} />
        <span className="text-[var(--text-dense-label)] font-semibold">Mission signals</span>
        <StatusLamp value={snapshot.missionOverall} kind="reach" />
        <DenseTag variant={status === 'NOMINAL' ? 'success' : status === 'CRITICAL' ? 'danger' : 'warning'}>
          {isLoading ? 'Probing…' : status}
        </DenseTag>
      </div>
      <div className={`mt-2 grid gap-2 ${compact ? 'grid-cols-1' : 'sm:grid-cols-3'}`}>
        <ReadinessChip
          label="Rocket"
          signal={snapshot.rocketOverall}
          detail={snapshot.release.detail}
          title="Rocket scope — Platform release, IB Gateway, supply chain"
        />
        <ReadinessChip
          label="Payload"
          signal={snapshot.payloadOverall}
          detail={snapshot.tradeProd.detail}
          title="Trade scope — per-env APIs, sockets, prod matrix"
        />
        <ReadinessChip
          label="Infra"
          signal={snapshot.infra.signal}
          detail={snapshot.infra.detail}
          title="Ground scope — cluster domains, PG/Redis, observability"
        />
      </div>
    </div>
  )
}
