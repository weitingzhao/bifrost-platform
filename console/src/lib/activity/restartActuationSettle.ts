import type { QueryClient } from '@tanstack/react-query'
import type { ClusterWorkload, ClusterWorkloadsResponse } from '@/api/clusterTypes'
import { updateActivityPhase } from '@/lib/activity/activityStore'
import type { ActivitySettledOutcome } from '@/lib/activity/activityTypes'
import {
  formatWorkloadRollout,
  isDeploymentRolloutComplete,
} from '@/lib/cluster/workloadRollout'

const SETTLE_POLL_MS = 2_000
const SETTLE_TIMEOUT_MS = 90_000

function readWorkload(
  queryClient: QueryClient,
  namespace: string,
  name: string,
): ClusterWorkload | null {
  const matches = queryClient.getQueriesData<ClusterWorkloadsResponse>({
    queryKey: ['cluster', 'workloads', namespace],
  })
  for (const [, data] of matches) {
    const w = data?.workloads?.find(
      x => x.name === name && x.kind.toLowerCase().includes('deploy'),
    )
    if (w != null) return w
  }
  return null
}

function progressDetail(w: ClusterWorkload | null, fallback: string): string {
  if (w == null) return fallback
  const line = formatWorkloadRollout(w)
  if (line != null) return line
  return `${w.status} · ready ${w.ready}`
}

/**
 * After a successful rollout-restart API: mark applying, poll cluster.
 * Progress detail updates every tick (updated/ready/available).
 * resolved = saw Progressing (or gen bump) then rollout complete; or ready string changed.
 */
export function startRestartActuationSettle(opts: {
  activityId: string
  queryClient: QueryClient
  namespace: string
  name: string
  baselineReady?: string | null
  baselineGeneration?: number | null
  apiMessage?: string
}): () => void {
  const { activityId, queryClient, namespace, name, apiMessage } = opts
  const baseline =
    opts.baselineReady ?? readWorkload(queryClient, namespace, name)?.ready ?? null
  const baselineGen =
    opts.baselineGeneration ??
    readWorkload(queryClient, namespace, name)?.generation ??
    null

  updateActivityPhase(activityId, 'applying', {
    detail: apiMessage
      ? `${apiMessage} — monitoring rollout…`
      : 'Pod rolling — monitoring rollout…',
  })

  let settled = false
  let sawProgressing = false
  let sawGenBump = baselineGen == null

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
    void queryClient.invalidateQueries({ queryKey: ['cluster', 'workloads', namespace] })
    void queryClient.invalidateQueries({ queryKey: ['satellite'] })
    const w = readWorkload(queryClient, namespace, name)
    if (w == null) return

    if (w.status === 'Progressing' || w.status === 'Unavailable') {
      sawProgressing = true
    }
    if (
      baselineGen != null &&
      w.generation != null &&
      w.generation > baselineGen
    ) {
      sawGenBump = true
    }

    const detail = progressDetail(w, 'Monitoring rollout…')
    updateActivityPhase(activityId, 'applying', { detail })

    const readyChanged =
      baseline != null && w.ready != null && w.ready !== baseline
    const complete = isDeploymentRolloutComplete(w)
    const observedCaughtUp =
      w.generation == null ||
      w.observed_generation == null ||
      w.observed_generation >= w.generation

    if (complete && observedCaughtUp && (sawProgressing || sawGenBump || readyChanged)) {
      finish(
        readyChanged && baseline != null
          ? `Ready ${baseline} → ${w.ready} · ${detail}`
          : `Rollout complete · ${detail}`,
        'resolved',
      )
    }
  }

  void queryClient.invalidateQueries({ queryKey: ['cluster', 'workloads', namespace] })
  const pollId = window.setInterval(tick, SETTLE_POLL_MS)
  // First tick soon — don't wait a full poll interval for the first progress line.
  window.setTimeout(tick, 400)
  const timeoutId = window.setTimeout(() => {
    const w = readWorkload(queryClient, namespace, name)
    const detail = progressDetail(w, 'ready unknown')
    if (w != null && isDeploymentRolloutComplete(w)) {
      finish(`Rollout complete · ${detail}`, 'resolved')
      return
    }
    if (baseline != null && (w == null || w.ready === baseline) && !sawProgressing) {
      finish(
        `Rollout requested — no progress after ${SETTLE_TIMEOUT_MS / 1000}s (${detail})`,
        'timeout',
      )
      return
    }
    finish(
      `Rollout requested — settle after ${SETTLE_TIMEOUT_MS / 1000}s (${detail})`,
      'resolved',
    )
  }, SETTLE_TIMEOUT_MS)

  return () => {
    settled = true
    window.clearInterval(pollId)
    window.clearTimeout(timeoutId)
  }
}
