import { useEffect, useRef } from 'react'
import { useQueries } from '@tanstack/react-query'
import { fetchPipelineRuns } from '@/api/delivery'
import type { DeliveryPipelineRunView } from '@/api/deliveryTypes'
import { upsertActivity } from '@/lib/activity/activityStore'
import { DELIVERY_TARGETS } from '@/lib/delivery/deliveryTargets'
import {
  isPipelineRunFailed,
  isPipelineRunSucceeded,
} from '@/lib/delivery/pipelineRunAskPack'

function linkForPipeline(pipeline: string): string {
  if (pipeline.includes('platform')) return 'platform-release'
  return 'trade-release'
}

function phaseForRun(run: DeliveryPipelineRunView): 'applying' | 'completed' | 'failed' {
  if (isPipelineRunSucceeded(run)) return 'completed'
  if (isPipelineRunFailed(run)) return 'failed'
  return 'applying'
}

/**
 * Shell-wide pipeline → Activity Feed bridge.
 * Dedup by run name; ignore cold-load of old terminal runs.
 */
export function usePipelineActivityBridge(): void {
  const tracked = useRef(new Map<string, 'applying' | 'completed' | 'failed'>())

  const queries = useQueries({
    queries: DELIVERY_TARGETS.map(t => ({
      queryKey: ['delivery', 'runs', t.pipeline],
      queryFn: () => fetchPipelineRuns(t.pipeline),
      refetchInterval: 20_000,
      staleTime: 10_000,
    })),
  })

  const runSignature = queries
    .map(q => {
      const run = q.data?.runs?.[0]
      if (run == null) return ''
      return `${run.name}|${run.status}|${run.reason ?? ''}|${run.completion_time ?? ''}`
    })
    .join(';;')

  const queriesRef = useRef(queries)
  queriesRef.current = queries

  useEffect(() => {
    DELIVERY_TARGETS.forEach((target, i) => {
      const run = queriesRef.current[i]?.data?.runs?.[0]
      if (run == null) return
      const id = `pipeline:${run.name}`
      const phase = phaseForRun(run)
      const prev = tracked.current.get(id)

      if (prev == null) {
        if (phase === 'applying') {
          tracked.current.set(id, phase)
          upsertActivity({
            id,
            kind: 'pipeline',
            phase: 'applying',
            title: `${target.shortLabel} pipeline`,
            target: run.name,
            detail: run.reason ?? 'Running',
            linkTo: linkForPipeline(target.pipeline),
            bumpTs: true,
          })
        } else {
          tracked.current.set(id, phase)
        }
        return
      }

      if (prev === phase) return
      tracked.current.set(id, phase)
      upsertActivity({
        id,
        kind: 'pipeline',
        phase,
        title: `${target.shortLabel} pipeline`,
        target: run.name,
        detail: run.reason ?? phase,
        linkTo: linkForPipeline(target.pipeline),
        settledOutcome: phase === 'completed' ? 'resolved' : phase === 'failed' ? 'error' : undefined,
        bumpTs: true,
      })
    })
  }, [runSignature])
}
