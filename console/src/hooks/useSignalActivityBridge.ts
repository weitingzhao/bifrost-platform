import { useEffect, useMemo, useRef } from 'react'
import { correlateActuationSettle } from '@/lib/activity/actuationSettleCorrelator'
import { upsertActivity } from '@/lib/activity/activityStore'
import {
  SignalTransitionDetector,
  signalTransitionToActivity,
  type ChipSnapshot,
} from '@/lib/activity/signalTransitionDetector'

/** Observe readiness chips → Activity Feed + smart settle correlator. */
export function useSignalActivityBridge(chips: ChipSnapshot[]): void {
  const detectorRef = useRef<SignalTransitionDetector | null>(null)
  if (detectorRef.current == null) {
    detectorRef.current = new SignalTransitionDetector()
  }

  const signature = useMemo(
    () =>
      chips
        .map(c => `${c.envScope ?? ''}:${c.label}:${c.signal}:${c.detail ?? ''}`)
        .join('|'),
    [chips],
  )

  useEffect(() => {
    const transitions = detectorRef.current!.observe(chips)
    for (const t of transitions) {
      upsertActivity(signalTransitionToActivity(t))
      if (t.to === 'ok') {
        correlateActuationSettle(t.chipLabel, t.envScope)
      }
    }
    // chips read from closure; signature gates re-entry
  }, [signature, chips])
}
