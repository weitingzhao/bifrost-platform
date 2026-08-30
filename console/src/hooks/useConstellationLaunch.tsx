import { useCallback, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ConfirmDialog } from '@bifrost/ui'
import { fetchPipelineRuns, startPipelineRun } from '@/api/delivery'
import type { DeliveryPipelineRunView } from '@/api/deliveryTypes'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'
import { payloadById, type PayloadId } from '@/lib/architecture/payloadConstellationCatalog'
import { deliveryTargetForPayload } from '@/hooks/useConstellationImpact'
import type { ConstellationImpact } from '@/lib/delivery/constellationImpact'
import {
  isPipelineRunFailed,
  isPipelineRunRunning,
  isPipelineRunSucceeded,
} from '@/lib/delivery/pipelineRunAskPack'

export const CONSTELLATION_FORMATION_QUERY_KEY = ['delivery', 'constellation-formation'] as const

type FormationPlan = {
  origin: PayloadId
  includeCompanions: PayloadId[]
  revision?: string
  tag?: string
  env: 'stg' | 'prod'
}

const POLL_MS = 4_000
const POLL_MAX = 90 // ~6 min

function sleep(ms: number) {
  return new Promise<void>(r => setTimeout(r, ms))
}

async function waitForRunTerminal(
  pipeline: string,
  runName: string,
): Promise<DeliveryPipelineRunView> {
  for (let i = 0; i < POLL_MAX; i++) {
    const data = await fetchPipelineRuns(pipeline)
    const run = data.runs?.find(r => r.name === runName) ?? data.runs?.[0]
    if (run != null && !isPipelineRunRunning(run)) {
      return run
    }
    await sleep(POLL_MS)
  }
  throw new Error(`Timed out waiting for ${pipeline} run ${runName}`)
}

/**
 * Formation flight: two independent pipeline runs (never merged Tekton).
 * Instruments enqueue before display-host; wait for instrument success before host.
 * Owner confirms; can decline suggested companions.
 */
export function useConstellationLaunch(impact: ConstellationImpact) {
  const qc = useQueryClient()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [plan, setPlan] = useState<FormationPlan | null>(null)
  const [includeSuggest, setIncludeSuggest] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [progress, setProgress] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: async (p: FormationPlan) => {
      const instruments: PayloadId[] = []
      const hosts: PayloadId[] = []
      const all = new Set<PayloadId>([p.origin, ...p.includeCompanions])
      for (const id of all) {
        if (payloadById(id).role === 'instrument') instruments.push(id)
        else hosts.push(id)
      }
      // Instrument-first, then display-host (plan W5)
      const order = [...instruments, ...hosts]

      const results: { payload: PayloadId; pipeline: string; run?: string }[] = []
      for (let i = 0; i < order.length; i++) {
        const id = order[i]!
        const targetId = deliveryTargetForPayload(id, p.env)
        const target = deliveryTargetById(targetId)
        const tag = id === 'research' ? p.tag : undefined
        const revision = id === 'research' ? undefined : p.revision
        setProgress(`Starting ${payloadById(id).label} (${target.pipeline})…`)
        void qc.setQueryData(CONSTELLATION_FORMATION_QUERY_KEY, {
          phase: 'running',
          current: id,
          order,
          index: i,
        })
        const resp = await startPipelineRun(target.pipeline, revision, undefined, tag)
        const runName = resp.run?.name
        results.push({ payload: id, pipeline: target.pipeline, run: runName })
        void qc.invalidateQueries({ queryKey: ['delivery', 'runs', target.pipeline] })

        const isLast = i === order.length - 1
        if (!isLast) {
          if (runName == null || runName === '') {
            throw new Error(`No run name returned for ${target.pipeline}`)
          }
          setProgress(`Waiting for ${payloadById(id).label} verify…`)
          const terminal = await waitForRunTerminal(target.pipeline, runName)
          if (isPipelineRunFailed(terminal)) {
            throw new Error(
              `${payloadById(id).label} pipeline failed (${terminal.status}) — companion not started`,
            )
          }
          if (!isPipelineRunSucceeded(terminal)) {
            throw new Error(
              `${payloadById(id).label} ended ${terminal.status} — companion not started`,
            )
          }
        }
      }
      return { order, results }
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['delivery', 'runs'] })
      void qc.setQueryData(CONSTELLATION_FORMATION_QUERY_KEY, { phase: 'done' })
      setConfirmOpen(false)
      setPlan(null)
      setError(null)
      setProgress(null)
    },
    onError: (e: Error) => {
      setError(e.message)
      setProgress(null)
      void qc.setQueryData(CONSTELLATION_FORMATION_QUERY_KEY, {
        phase: 'error',
        error: e.message,
      })
    },
  })

  const requestLaunch = useCallback(
    (opts: { revision?: string; tag?: string; env?: 'stg' | 'prod' }) => {
      setIncludeSuggest(true)
      setError(null)
      setProgress(null)
      setPlan({
        origin: impact.origin,
        includeCompanions: impact.flyWith,
        revision: opts.revision,
        tag: opts.tag,
        env: opts.env ?? 'stg',
      })
      setConfirmOpen(true)
    },
    [impact.origin, impact.flyWith],
  )

  const resolveCompanions = (): PayloadId[] => {
    if (plan == null) return []
    if (includeSuggest) return plan.includeCompanions
    return plan.includeCompanions.filter(id => {
      const row = impact.rows.find(r => r.payload === id)
      return row?.verdict === 'must'
    })
  }

  const companionLabels =
    plan?.includeCompanions.map(id => payloadById(id).label).join(', ') ?? ''
  const skipLabels = impact.rows
    .filter(r => r.verdict === 'skip')
    .map(r => r.label)
    .join(', ')

  const message =
    plan == null
      ? ''
      : [
          `Origin: ${payloadById(plan.origin).label}`,
          impact.summary,
          plan.includeCompanions.length > 0
            ? includeSuggest
              ? `Will include companions: ${companionLabels}`
              : `Companions declined (must-only): ${
                  resolveCompanions()
                    .map(id => payloadById(id).label)
                    .join(', ') || 'none'
                }`
            : 'No companions — origin only.',
          skipLabels ? `Skipped: ${skipLabels}` : '',
          'Two independent pipelines — never a merged Tekton graph.',
          'Instruments run first; display-host starts after verify succeeds.',
          progress != null ? progress : '',
          error != null ? `Error: ${error}` : '',
        ]
          .filter(Boolean)
          .join('\n')

  const dialog =
    plan != null && confirmOpen ? (
      <ConfirmDialog
        open={confirmOpen}
        title="Confirm constellation formation"
        message={message}
        confirmLabel={mutation.isPending ? 'Launching…' : 'Confirm launch'}
        confirming={mutation.isPending}
        bodyExtra={
          plan.includeCompanions.length > 0 ? (
            <label className="mt-2 flex items-center gap-2 text-dense-meta">
              <input
                type="checkbox"
                checked={includeSuggest}
                disabled={mutation.isPending}
                onChange={e => setIncludeSuggest(e.target.checked)}
              />
              <span>Include suggested companions ({companionLabels})</span>
            </label>
          ) : null
        }
        onConfirm={() => {
          mutation.mutate({
            ...plan,
            includeCompanions: resolveCompanions(),
          })
        }}
        onCancel={() => {
          if (mutation.isPending) return
          setConfirmOpen(false)
          setPlan(null)
        }}
      />
    ) : null

  return {
    requestLaunch,
    dialog,
    isPending: mutation.isPending,
    error,
    progress,
  }
}
