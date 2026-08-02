import { useEffect, useState } from 'react'
import type { ComputeOffCycleHint } from '@/lib/cluster/nodeWizard'

/**
 * Tracks whether the selected compute node has been observed offline this session.
 * After wake (back online), procedure advances to Uncordon instead of re-offering Power off.
 */
export function useComputeOffCycleHint(
  nodeName: string | null | undefined,
  powerState: string | undefined,
  nodeStatus: string | undefined,
  cordoned: boolean | undefined,
): ComputeOffCycleHint {
  const [trackedName, setTrackedName] = useState<string | null>(null)
  const [sawOffline, setSawOffline] = useState(false)

  useEffect(() => {
    const next = nodeName ?? null
    if (next !== trackedName) {
      setTrackedName(next)
      setSawOffline(false)
    }
  }, [nodeName, trackedName])

  useEffect(() => {
    if (nodeName == null || nodeName === '') return
    const offline = powerState === 'offline' || (nodeStatus != null && nodeStatus !== 'Ready')
    if (offline) {
      setSawOffline(true)
      return
    }
    if (cordoned !== true) {
      setSawOffline(false)
    }
  }, [nodeName, powerState, nodeStatus, cordoned])

  const online = powerState === 'online' || nodeStatus === 'Ready'
  const ready = nodeStatus === 'Ready'
  if (sawOffline && online && ready) return 'post_wake'
  return 'pre_poweroff'
}
