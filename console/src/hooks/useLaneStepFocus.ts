import { useCallback, useEffect, useRef, useState } from 'react'
import type { LaneDetailReason } from '@/lib/delivery/laneDetailContext'
import { resolveInitialLaneStep } from '@/lib/delivery/initialLaneStep'
import type { StepStatus } from '@/lib/delivery/releaseStepTypes'

/**
 * Step selection state for the lane detail pages with one-shot smart focus.
 *
 * Step data loads async, so the initial index cannot be derived in a
 * `useState` initializer. Once the backing queries settle (`ready`), the hook
 * auto-focuses the most relevant step exactly once — later refetches never
 * move the selection, and a manual click permanently disables auto focus.
 */
export function useLaneStepFocus({
  statuses,
  ready,
  reason,
}: {
  statuses: readonly StepStatus[]
  ready: boolean
  reason: LaneDetailReason
}): [number, (index: number) => void] {
  const [activeIndex, setActiveIndex] = useState(0)
  const userSelectedRef = useRef(false)
  const autoFocusedRef = useRef(false)

  useEffect(() => {
    if (!ready || autoFocusedRef.current || userSelectedRef.current) return
    autoFocusedRef.current = true
    setActiveIndex(resolveInitialLaneStep(statuses, reason))
  }, [ready, statuses, reason])

  const selectStep = useCallback((index: number) => {
    userSelectedRef.current = true
    setActiveIndex(index)
  }, [])

  return [activeIndex, selectStep]
}
