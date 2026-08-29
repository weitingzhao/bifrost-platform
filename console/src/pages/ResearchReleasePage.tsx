import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { DenseTag } from '@bifrost/ui'
import { fetchDeliveryPipelines, fetchPipelineRuns, startPipelineRun } from '@/api/delivery'
import { PipelineRunsPanel } from '@/components/delivery/PipelineRunsPanel'
import { OpsSection } from '@/components/layout/OpsSection'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { LaunchGateBar } from '@/components/task-mode/LaunchGateBar'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { hasDeliverInFlight, launchVerdictToSignal } from '@/lib/task-mode/satelliteLaunchVerdict'
import {
  RESEARCH_DEFAULT_TAG,
  buildResearchLaunchCheckpoints,
  resolveResearchLaunchVerdict,
} from '@/lib/task-mode/researchLaunchVerdict'

const RESEARCH_PIPELINE = 'bifrost-deliver-research'

/**
 * Launch Research — payload-grade desk (peer to Satellite).
 *
 *   mirror-sync → clone → kaniko → rollout → verify → gitops-sync
 *
 * Order: image must exist in the registry BEFORE k8s/api/deployment.yaml is
 * bumped (Argo CD syncs bifrost-research from GitHub automatically).
 *
 * D10: observe-only. This page touches the research namespace only.
 */
export function ResearchReleasePage() {
  const { canOperate } = usePlatformAuth()
  const [tag, setTag] = useState(RESEARCH_DEFAULT_TAG)
  const [starting, setStarting] = useState(false)
  const [feedback, setFeedback] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const pipelinesQ = useQuery({
    queryKey: ['delivery', 'pipelines'],
    queryFn: fetchDeliveryPipelines,
    refetchInterval: 30_000,
  })
  const runsQ = useQuery({
    queryKey: ['delivery', 'runs', RESEARCH_PIPELINE],
    queryFn: () => fetchPipelineRuns(RESEARCH_PIPELINE),
    refetchInterval: 15_000,
  })

  const pipelinePresent = (pipelinesQ.data?.pipelines ?? []).some(
    p => p.name === RESEARCH_PIPELINE,
  )
  const deliverInFlight = hasDeliverInFlight(runsQ.data?.runs)

  const verdictInput = useMemo(
    () => ({
      canOperate,
      pipelinePresent,
      tag,
      deliverInFlight,
    }),
    [canOperate, pipelinePresent, tag, deliverInFlight],
  )
  const verdict = resolveResearchLaunchVerdict(verdictInput)
  const checkpoints = buildResearchLaunchCheckpoints(verdictInput)
  const readyCount = checkpoints.filter(c => c.ok).length

  async function launch() {
    if (verdict.kind !== 'GO') return
    const t = tag.trim()
    setError(null)
    setFeedback(null)
    setStarting(true)
    try {
      const res = await startPipelineRun(RESEARCH_PIPELINE, 'main', undefined, t)
      setFeedback(
        res.ok
          ? `Started ${RESEARCH_PIPELINE} for tag ${t}. Bump k8s/api/deployment.yaml only after the image lands.`
          : (res.message ?? 'Start failed'),
      )
      if (!res.ok) setError(res.message ?? 'Start failed')
      void runsQ.refetch()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Start failed')
    } finally {
      setStarting(false)
    }
  }

  const lamp = pipelinesQ.isLoading ? 'unknown' : launchVerdictToSignal(verdict.kind)
  const tagLabel =
    verdict.kind === 'GO' ? 'GO' : verdict.kind === 'IN_FLIGHT' ? 'IN FLIGHT' : 'NO-GO'
  const tagVariant =
    verdict.kind === 'GO' ? 'success' : verdict.kind === 'IN_FLIGHT' ? 'warning' : 'danger'

  return (
    <>
      <OpsVerdictStrip
        title="LAUNCH RESEARCH"
        lamp={lamp}
        tagLabel={`${tagLabel} · ${readyCount}/${checkpoints.length}`}
        tagVariant={tagVariant}
        summary={verdict.detail}
        meta={
          <span className="inline-flex items-center gap-1.5">
            <DenseTag variant="category" size="cell">
              second payload
            </DenseTag>
            <span className="text-[var(--muted-foreground)]">
              mirror-sync → clone → kaniko → rollout → verify → gitops-sync
            </span>
          </span>
        }
      />

      <OpsSection title="Launch gate">
        <LaunchGateBar
          verdict={verdict}
          checkpoints={checkpoints}
          onLaunch={() => void launch()}
          launchLabel="Launch Research"
          blockedLabel="Launch blocked"
          launchPending={starting}
          launchDisabled={verdict.kind !== 'GO' || starting}
          launchDisabledReason={verdict.disabledReason}
          onOpenActiveRun={
            verdict.kind === 'IN_FLIGHT'
              ? () => document.getElementById('research-launch-runs')?.scrollIntoView({
                  behavior: 'smooth',
                  block: 'start',
                })
              : undefined
          }
        />

        <div className="mt-3 flex flex-wrap items-end gap-2">
          <label className="flex flex-col gap-1">
            <span className="text-dense-caption text-[var(--muted-foreground)]">Image tag</span>
            <input
              value={tag}
              onChange={e => setTag(e.target.value)}
              placeholder="0.48.4"
              className="h-7 w-32 rounded border border-[var(--border)] bg-[var(--card)] px-2 font-mono text-sm"
              aria-label="Image tag"
            />
          </label>
        </div>

        <p className="mt-2 text-dense-caption leading-snug text-[var(--muted-foreground)]">
          Order matters: build the image first, confirm the tag is in the registry, then bump{' '}
          <code className="font-mono">k8s/api/deployment.yaml</code> and push. The Argo CD
          Application syncs automatically from GitHub, so pinning a missing tag will take
          research-api down. Default Tekton tag <code className="font-mono">dev</code> is a
          moving smoke tag and must not pin the Deployment.
        </p>

        {error ? (
          <p className="mt-2 text-dense-meta text-[var(--destructive)]">{error}</p>
        ) : null}
        {feedback && !error ? (
          <p className="mt-2 text-dense-meta text-[var(--muted-foreground)]">{feedback}</p>
        ) : null}
      </OpsSection>

      <div id="research-launch-runs">
        <PipelineRunsPanel
          pipelines={pipelinesQ.data}
          pipelinesLoading={pipelinesQ.isLoading}
          errorMessage={pipelinesQ.error instanceof Error ? pipelinesQ.error.message : undefined}
          layout="observe"
          preferPipeline={RESEARCH_PIPELINE}
        />
      </div>
    </>
  )
}
