import type { ReactNode } from 'react'
import { Bot, Radar, Rocket, Server, type LucideIcon } from 'lucide-react'
import { Button } from '@bifrost/ui'
import type { MissionSnapshot, ModuleState } from '@/lib/control-room/missionSignals'
import { signalColor } from '@/lib/control-room/missionSignals'

const ROCKET_MODULES: Array<{
  key: keyof Pick<MissionSnapshot, 'infra' | 'release' | 'control' | 'agent'>
  icon: LucideIcon
  name: string
  role: string
}> = [
  {
    key: 'infra',
    icon: Server,
    name: 'Infra',
    role: 'Runtime foundation — K3s cluster, nodes, workloads',
  },
  {
    key: 'release',
    icon: Rocket,
    name: 'Release',
    role: 'Launch pipeline — CI/CD, deliver, STG smoke',
  },
  {
    key: 'control',
    icon: Radar,
    name: 'Control',
    role: 'Flight computer — platform-api, console, GitOps',
  },
  {
    key: 'agent',
    icon: Bot,
    name: 'Agent',
    role: 'Autopilot — remediation runner, git bridge, drift repair',
  },
]

function RocketModuleCard({
  icon: Icon,
  name,
  role,
  state,
  onClick,
  footer,
}: {
  icon: LucideIcon
  name: string
  role: string
  state: ModuleState
  onClick: () => void
  footer?: ReactNode
}) {
  return (
    <div className="mission-rocket-card-wrap">
      <button type="button" className="mission-rocket-card" onClick={onClick} title={state.detail}>
        <Icon size={20} style={{ color: signalColor(state.signal) }} className="mission-rocket-card-icon" />
        <div className="mission-rocket-card-body">
          <div className="mission-rocket-card-name">{name}</div>
          <div className="mission-rocket-card-val">{state.value}</div>
          <div className="mission-rocket-card-role">{role}</div>
        </div>
      </button>
      {footer}
    </div>
  )
}

export interface RocketSubsystemsGridProps {
  snapshot: MissionSnapshot
  onOpenCluster: () => void
  onOpenDelivery: () => void
  onOpenPlatformRelease: () => void
  onOpenAgentDesk: () => void
  onDispatchReleaseAgent?: () => void
  releaseDispatchPending?: boolean
  canDispatchRelease?: boolean
}

export function RocketSubsystemsGrid({
  snapshot,
  onOpenCluster,
  onOpenDelivery,
  onOpenPlatformRelease,
  onOpenAgentDesk,
  onDispatchReleaseAgent,
  releaseDispatchPending = false,
  canDispatchRelease = false,
}: RocketSubsystemsGridProps) {
  const handlers: Record<(typeof ROCKET_MODULES)[number]['key'], () => void> = {
    infra: onOpenCluster,
    release: onOpenDelivery,
    control: onOpenPlatformRelease,
    agent: onOpenAgentDesk,
  }

  const releaseDegraded = snapshot.release.signal !== 'ok'

  return (
    <div className="mission-rocket-grid">
      {ROCKET_MODULES.map(mod => {
        const footer =
          mod.key === 'release' && releaseDegraded && onDispatchReleaseAgent != null ? (
            <Button
              type="button"
              variant="outline"
              size="xs"
              className="mission-rocket-release-agent"
              disabled={!canDispatchRelease || releaseDispatchPending}
              onClick={e => {
                e.stopPropagation()
                onDispatchReleaseAgent()
              }}
            >
              {releaseDispatchPending ? 'Starting…' : 'Platform release (Agent)'}
            </Button>
          ) : undefined

        return (
          <RocketModuleCard
            key={mod.key}
            icon={mod.icon}
            name={mod.name}
            role={mod.role}
            state={snapshot[mod.key]}
            onClick={handlers[mod.key]}
            footer={footer}
          />
        )
      })}
    </div>
  )
}
