import { Rocket } from 'lucide-react'
import { Button } from '@bifrost/ui'
import type { ModuleState } from '@/lib/control-room/missionSignals'
import { signalColor } from '@/lib/control-room/missionSignals'

interface ReleaseAgentCalloutProps {
  release: ModuleState
  onDispatch: () => void
  pending?: boolean
  canDispatch?: boolean
}

/** Diagnosis-zone CTA when Rocket · Release is degraded. */
export function ReleaseAgentCallout({
  release,
  onDispatch,
  pending = false,
  canDispatch = false,
}: ReleaseAgentCalloutProps) {
  if (release.signal === 'ok') return null

  return (
    <div className="control-room-release-callout">
      <Rocket size={16} style={{ color: signalColor(release.signal) }} />
      <div className="control-room-release-callout__body">
        <span className="control-room-release-callout__title">Release pipeline needs attention</span>
        <span className="control-room-release-callout__detail">{release.detail}</span>
      </div>
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={!canDispatch || pending}
        onClick={onDispatch}
      >
        {pending ? 'Starting…' : 'Platform release (Agent)'}
      </Button>
    </div>
  )
}
