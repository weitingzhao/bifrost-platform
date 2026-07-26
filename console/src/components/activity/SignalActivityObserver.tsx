import { useSignalActivityBridge } from '@/hooks/useSignalActivityBridge'
import type { ChipSnapshot } from '@/lib/activity/signalTransitionDetector'
import { normalizeActivityEnvScope } from '@/lib/activity/signalTransitionDetector'

/** Mount beside readiness chip rows so transitions are observed even when FixBar is idle/null. */
export function SignalActivityObserver({
  chips,
  envScope,
}: {
  chips: Array<Pick<ChipSnapshot, 'label' | 'signal' | 'detail'>>
  /** Panel env so STG/PROD same labels do not share Activity ids. */
  envScope: string
}) {
  const scope = normalizeActivityEnvScope(envScope)
  const scoped: ChipSnapshot[] = chips.map(c => ({
    ...c,
    envScope: scope,
  }))
  useSignalActivityBridge(scoped)
  return null
}
