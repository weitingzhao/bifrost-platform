import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, DenseTag } from '@bifrost/ui'
import { fetchDeliveryPipelines, startPipelineRun } from '@/api/delivery'
import { PipelineRunsPanel } from '@/components/delivery/PipelineRunsPanel'
import { OpsSection } from '@/components/layout/OpsSection'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

const RESEARCH_PIPELINE = 'bifrost-deliver-research'

/**
 * Launch Research — release surface for the second payload.
 *
 * Research is peer to Satellite (execution payload), not a subcontractor; it
 * therefore gets a payload-grade chain rather than the plugin-grade clone+kaniko:
 *
 *   mirror-sync → clone → kaniko → rollout → verify → gitops-sync
 *
 * Release order is not a style preference — it is the rule that broke DEV once:
 * the image must exist in the registry BEFORE k8s/api/deployment.yaml is bumped,
 * because the bifrost-research Argo CD Application has automated sync straight
 * from GitHub. Pushing a manifest is changing the running cluster.
 *
 * D10: observe-only. This page touches the research namespace only.
 */
export function ResearchReleasePage() {
  const { canOperate } = usePlatformAuth()
  const [tag, setTag] = useState('')
  const [starting, setStarting] = useState(false)
  const [feedback, setFeedback] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const pipelinesQ = useQuery({
    queryKey: ['delivery', 'pipelines'],
    queryFn: fetchDeliveryPipelines,
    refetchInterval: 30_000,
  })

  const pipelinePresent = (pipelinesQ.data?.pipelines ?? []).some(
    p => p.name === RESEARCH_PIPELINE,
  )

  async function launch() {
    const t = tag.trim()
    if (t === '') {
      setError('Image tag required — pass the version you intend to deploy (e.g. 0.30.0).')
      return
    }
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
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Start failed')
    } finally {
      setStarting(false)
    }
  }

  return (
    <>
      <OpsVerdictStrip
        title="LAUNCH RESEARCH"
        lamp={pipelinePresent ? 'ok' : 'degraded'}
        tagLabel={pipelinePresent ? 'READY' : 'MISSING'}
        tagVariant={pipelinePresent ? 'success' : 'warning'}
        summary={
          pipelinePresent
            ? 'Research delivery chain available.'
            : `${RESEARCH_PIPELINE} not found in the cluster — apply k8s/cicd/tekton/pipeline-deliver-research.yaml.`
        }
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

      <OpsSection title="Launch">
        <div className="flex flex-wrap items-end gap-2">
          <label className="flex flex-col gap-1">
            <span className="text-dense-caption text-[var(--muted-foreground)]">Image tag</span>
            <input
              value={tag}
              onChange={e => setTag(e.target.value)}
              placeholder="0.30.0"
              className="h-7 w-32 rounded border border-[var(--border)] bg-[var(--card)] px-2 font-mono text-sm"
              aria-label="Image tag"
            />
          </label>
          <Button
            size="sm"
            disabled={!canOperate || starting || !pipelinePresent}
            onClick={() => void launch()}
          >
            {starting ? 'Starting…' : 'Launch Research'}
          </Button>
          {!canOperate ? (
            <span className="text-dense-caption text-[var(--muted-foreground)]">
              Operator role required.
            </span>
          ) : null}
        </div>

        <p className="mt-2 text-dense-caption leading-snug text-[var(--muted-foreground)]">
          Order matters: build the image first, confirm the tag is in the registry, then bump{' '}
          <code className="font-mono">k8s/api/deployment.yaml</code> and push. The Argo CD
          Application syncs automatically from GitHub, so pushing a manifest that points at a
          tag which does not exist yet will take research-api down.
        </p>

        {error ? (
          <p className="mt-2 text-dense-meta text-[var(--destructive)]">{error}</p>
        ) : null}
        {feedback && !error ? (
          <p className="mt-2 text-dense-meta text-[var(--muted-foreground)]">{feedback}</p>
        ) : null}
      </OpsSection>

      <PipelineRunsPanel
        pipelines={pipelinesQ.data}
        pipelinesLoading={pipelinesQ.isLoading}
        errorMessage={pipelinesQ.error instanceof Error ? pipelinesQ.error.message : undefined}
        layout="operate"
      />
    </>
  )
}
