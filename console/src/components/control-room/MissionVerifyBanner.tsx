import { CheckCircle2, X, AlertTriangle } from 'lucide-react'
import type { MissionVerifyBannerState } from '@/hooks/useMissionVerification'

interface MissionVerifyBannerProps {
  state: MissionVerifyBannerState
  onDismiss: () => void
  onOpenJob?: (jobId: string) => void
}

export function MissionVerifyBanner({ state, onDismiss, onOpenJob }: MissionVerifyBannerProps) {
  const Icon = state.nominal ? CheckCircle2 : state.jobStatus === 'failed' ? AlertTriangle : AlertTriangle
  const tone = state.nominal ? 'nominal' : state.jobStatus === 'failed' ? 'fail' : 'caution'

  return (
    <div className={`mission-verify-banner mission-verify-banner--${tone}`} role="status">
      <Icon size={18} className="mission-verify-banner__icon" />
      <div className="mission-verify-banner__body">
        <div className="mission-verify-banner__headline">{state.headline}</div>
        <div className="mission-verify-banner__detail">{state.detail}</div>
      </div>
      {onOpenJob != null && (
        <button type="button" className="mission-verify-banner__link" onClick={() => onOpenJob(state.jobId)}>
          View job
        </button>
      )}
      <button type="button" className="mission-verify-banner__dismiss" onClick={onDismiss} aria-label="Dismiss">
        <X size={14} />
      </button>
    </div>
  )
}
