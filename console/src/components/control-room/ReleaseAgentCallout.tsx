import { Rocket } from 'lucide-react'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import type { ModuleState } from '@/lib/control-room/missionSignals'
import { signalColor } from '@/lib/control-room/missionSignals'

interface ReleaseAgentCalloutProps {
  release: ModuleState
  onDispatch: () => void
  pending?: boolean
  canDispatch?: boolean
  disabledReason?: string
}

/** Diagnosis-zone CTA when Rocket · Release is degraded. */
export function ReleaseAgentCallout({
  release,
  onDispatch,
  pending = false,
  canDispatch = false,
  disabledReason,
}: ReleaseAgentCalloutProps) {
  if (release.signal === 'ok') return null

  return (
    <div className="control-room-release-callout">
      <Rocket size={16} style={{ color: signalColor(release.signal) }} />
      <div className="control-room-release-callout__body">
        <span className="control-room-release-callout__title">Release pipeline needs attention</span>
        <span className="control-room-release-callout__detail">{release.detail}</span>
      </div>
      <AgentTriggerButton
        label="AI Release"
        pending={pending}
        disabled={!canDispatch}
        title={disabledReason ?? 'Run Platform · Release agent task'}
        onClick={onDispatch}
      />
    </div>
  )
}
