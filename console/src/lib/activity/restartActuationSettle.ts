import type { QueryClient } from '@tanstack/react-query'
import type { ClusterWorkloadsResponse } from '@/api/clusterTypes'
import { updateActivityPhase } from '@/lib/activity/activityStore'
import type { ActivitySettledOutcome } from '@/lib/activity/activityTypes'

const SETTLE_POLL_MS = 5_000
const SETTLE_TIMEOUT_MS = 30_000

function readWorkloadReady(
  queryClient: QueryClient,
  namespace: string,
  name: string,
): string | null {
  const cached = queryClient.getQueryData<ClusterWorkloadsResponse>([
    'cluster',
    'workloads',
    namespace,
  ])
  const w = cached?.workloads?.find(
    x => x.name === name && x.kind.toLowerCase().includes('deploy'),
  )
  return w?.ready ?? null
}

/**
 * After a successful rollout-restart API: mark applying, poll cluster for ~30s.
 * resolved = ready string changed; timeout = baseline existed but ready unchanged;
 * resolved (no baseline) when we cannot compare.
 */
export function startRestartActuationSettle(opts: {
  activityId: string
  queryClient: QueryClient
  namespace: string
  name: string
  baselineReady?: string | null
  apiMessage?: string
}): () => void {
  const { activityId, queryClient, namespace, name, apiMessage } = opts
  const baselineReady =
    opts.baselineReady ?? readWorkloadReady(queryClient, namespace, name)

  updateActivityPhase(activityId, 'applying', {
    detail: apiMessage
      ? `${apiMessage} — monitoring ready…`
      : 'Pod rolling — monitoring ready…',
  })

  let settled = false
  const finish = (detail: string, settledOutcome: ActivitySettledOutcome) => {
    if (settled) return
    settled = true
    window.clearInterval(pollId)
    window.clearTimeout(timeoutId)
    updateActivityPhase(activityId, 'settled', {
      settledOutcome,
      detail,
    })
  }

  const tick = () => {
    void queryClient.invalidateQueries({ queryKey: ['cluster'] })
    void queryClient.invalidateQueries({ queryKey: ['satellite'] })
    const ready = readWorkloadReady(queryClient, namespace, name)
    if (baselineReady != null && ready != null && ready !== baselineReady) {
      finish(`Ready ${baselineReady} → ${ready}`, 'resolved')
    }
  }

  void queryClient.invalidateQueries({ queryKey: ['cluster'] })
  const pollId = window.setInterval(tick, SETTLE_POLL_MS)
  const timeoutId = window.setTimeout(() => {
    const ready = readWorkloadReady(queryClient, namespace, name)
    if (baselineReady != null && (ready == null || ready === baselineReady)) {
      finish(
        ready != null
          ? `Rollout requested — ready unchanged (${ready}) after ${SETTLE_TIMEOUT_MS / 1000}s`
          : `Rollout requested — ready unknown after ${SETTLE_TIMEOUT_MS / 1000}s`,
        'timeout',
      )
      return
    }
    // No baseline to compare — API accepted; settle resolved.
    finish(
      ready != null
        ? `Rollout requested — ready ${ready} after ${SETTLE_TIMEOUT_MS / 1000}s`
        : `Rollout requested — settle after ${SETTLE_TIMEOUT_MS / 1000}s`,
      'resolved',
    )
  }, SETTLE_TIMEOUT_MS)

  return () => {
    settled = true
    window.clearInterval(pollId)
    window.clearTimeout(timeoutId)
  }
}
