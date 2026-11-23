import { useSharedObservation } from '@/domains/foundation/runtime-observation/sharedObservationStore'

/**
 * The one sentence every frame's second row says when Signals holds a pinned snapshot (箭10 2026-11-23, gap table
 * item 9: a pin is kept across places by the shared context above the routes, sharedObservationStore). The place's
 * own reads stay live; this only says Signals still holds what was pinned, and when it was captured. Nothing when
 * Signals has not been opened in this tab, or is live. A phone's row has no room for the sentence: "Pinned · HH:MM".
 */
export function SignalsPinNote({ className = '' }: { className?: string }) {
  const shared = useSharedObservation()
  if (shared.state !== 'ready' || !shared.observation.pinned) return null
  const at = shared.observation.snapshot.capturedAt
  const hhmm = Number.isFinite(Date.parse(at)) ? new Date(at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false }) : at
  return (
    <a href="#observation" className={className} data-signals-pin title={`Signals snapshot captured ${at}, pinned`}>
      <span className="hidden sm:inline">Signals snapshot retained · </span><span className="sm:hidden">Pinned · </span>{hhmm}
    </a>
  )
}
