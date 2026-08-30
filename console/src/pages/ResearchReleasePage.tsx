import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, DenseTag } from '@bifrost/ui'
import {
  fetchDeliveryPipelines,
  fetchPipelineRunLogs,
  fetchPipelineRunSteps,
  fetchPipelineRuns,
} from '@/api/delivery'
import { fetchResearchHealth, isResearchProxyError } from '@/api/researchEngine'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { ConstellationStrip } from '@/components/delivery/ConstellationStrip'
import { DeliveryActiveRunPanel } from '@/components/delivery/DeliveryActiveRunPanel'
import {
  LaneDetailCollapse,
  LaneDetailContextStrip,
  LaneStateStrip,
} from '@/components/delivery/LaneDetailShell'
import { LaneOperateSplit } from '@/components/delivery/LaneOperateSplit'
import { PluginStepCommandCenter } from '@/components/delivery/PluginStepCommandCenter'
import { LaunchGateBar } from '@/components/task-mode/LaunchGateBar'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { useLaneStepFocus } from '@/hooks/useLaneStepFocus'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  isAmbientAgentActive,
  type AmbientAgentShellProps,
} from '@/lib/agent/ambientAgent'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import {
  buildResearchDeployPrompt,
  RESEARCH_DEPLOY_SCOPE,
} from '@/lib/agent/researchDeployAgentPrompt'
import { readLaneDetailReasonFromLocation } from '@/lib/delivery/laneDetailContext'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'
import { useConstellationImpact } from '@/hooks/useConstellationImpact'
import { useConstellationLaunch } from '@/hooks/useConstellationLaunch'
import {
  formatPipelineRunStatus,
  isPipelineRunFailed,
  isPipelineRunRunning,
  isPipelineRunSucceeded,
} from '@/lib/delivery/pipelineRunAskPack'
import {
  deriveResearchLaunchSteps,
  isResearchImageLandedLog,
} from '@/lib/task-mode/researchLaunchSteps'
import {
  RESEARCH_DEFAULT_TAG,
  buildResearchLaunchCheckpoints,
  isResearchReleaseTag,
  resolveResearchLaunchVerdict,
} from '@/lib/task-mode/researchLaunchVerdict'
import { hasDeliverInFlight } from '@/lib/task-mode/satelliteLaunchVerdict'

const AI_DEPLOY_LABEL = 'AI Deploy Research'
const AI_DEPLOY_TASK_LABEL = scopeToLabel(RESEARCH_DEPLOY_SCOPE)
const RESEARCH_TARGET = deliveryTargetById('research')

type ResearchReleasePageProps = AmbientAgentShellProps & {
  onOpenResearchEngine?: () => void
}

/**
 * Launch Research — Satellite instrument desk (OLAP payload on Trade display-host).
 * Agent owns Build; Pin waits until the image is in the registry (Argo auto-sync).
 */
export function ResearchReleasePage({
  ambientJobId,
  ambientJobScope,
  ambientJobStatus,
  onStartAgentJob,
  onExpandAgentDock,
  onOpenResearchEngine,
}: ResearchReleasePageProps) {
  const { canOperate } = usePlatformAuth()
  const [detailReason] = useState(readLaneDetailReasonFromLocation)
  const [tag, setTag] = useState(RESEARCH_DEFAULT_TAG)

  const agentInFlight =
    isAmbientAgentActive(ambientJobId, ambientJobStatus) &&
    ambientJobScope === RESEARCH_DEPLOY_SCOPE

  const pipelinesQ = useQuery({
    queryKey: ['delivery', 'pipelines'],
    queryFn: fetchDeliveryPipelines,
    refetchInterval: 30_000,
  })
  const runsQ = useQuery({
    queryKey: ['delivery', 'runs', RESEARCH_TARGET.pipeline],
    queryFn: () => fetchPipelineRuns(RESEARCH_TARGET.pipeline),
    refetchInterval: 8_000,
  })
  const latestRun = runsQ.data?.runs?.[0]
  const deliverInFlight = hasDeliverInFlight(runsQ.data?.runs)

  const stepsQ = useQuery({
    queryKey: ['delivery', 'steps', latestRun?.name, 'research-desk'],
    queryFn: () => fetchPipelineRunSteps(latestRun!.name, latestRun!.namespace),
    enabled: latestRun?.name != null && latestRun.name !== '',
    refetchInterval: latestRun != null && isPipelineRunRunning(latestRun) ? 4_000 : 20_000,
  })

  const logsQ = useQuery({
    queryKey: ['delivery', 'logs', RESEARCH_TARGET.pipeline, latestRun?.name, 'active'],
    queryFn: () => fetchPipelineRunLogs(latestRun!.name, latestRun!.namespace),
    enabled: latestRun != null && isPipelineRunFailed(latestRun),
  })

  const healthQ = useQuery({
    queryKey: ['research', 'health', 'launch-desk'],
    queryFn: fetchResearchHealth,
    refetchInterval: 20_000,
  })
  const health = healthQ.data != null && !isResearchProxyError(healthQ.data) ? healthQ.data : null

  const constellationImpact = useConstellationImpact({
    origin: 'research',
    revision: isResearchReleaseTag(tag) ? tag.trim() : 'main',
    changedRepos: ['bifrost-research'],
  })
  const formation = useConstellationLaunch(constellationImpact)

  const verdictInput = useMemo(
    () => ({
      canOperate,
      pipelinePresent: (pipelinesQ.data?.pipelines ?? []).some(
        p => p.name === RESEARCH_TARGET.pipeline,
      ),
      tag,
      deliverInFlight,
      agentInFlight,
    }),
    [canOperate, pipelinesQ.data?.pipelines, tag, deliverInFlight, agentInFlight],
  )
  const verdict = resolveResearchLaunchVerdict(verdictInput)
  const checkpoints = buildResearchLaunchCheckpoints(verdictInput)

  const cycleSteps = deriveResearchLaunchSteps({
    run: latestRun,
    phases: stepsQ.data?.phases,
    desiredTag: tag,
    liveVersion: health?.version ?? null,
    liveOk: health?.startup_ok === true,
    agentInFlight,
    imageLandedHint: isResearchImageLandedLog(logsQ.data?.logs),
  })
  const [activeIndex, setActiveIndex] = useLaneStepFocus({
    statuses: cycleSteps.map(s => s.status),
    ready: !runsQ.isLoading,
    reason: detailReason,
  })

  const aiDeploy = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    ambientJobStatus,
    onStartAgentJob,
    scope: RESEARCH_DEPLOY_SCOPE,
    label: AI_DEPLOY_TASK_LABEL,
    buildRequest: () => ({
      prompt: buildResearchDeployPrompt({ tag, latestRun }),
    }),
  })

  const deployDispatchAllowed = !aiDeploy.disabled && verdict.kind === 'GO'
  const deployDisabledReason =
    verdict.kind !== 'GO'
      ? (verdict.disabledReason ?? verdict.detail)
      : aiDeploy.disabledReason

  function handleAiDeployClick() {
    if (agentInFlight) {
      onExpandAgentDock?.()
      return
    }
    if (!deployDispatchAllowed) return
    aiDeploy.trigger()
  }

  const evidenceLinks =
    onOpenResearchEngine != null ? (
      <div className="flex min-w-0 flex-wrap items-center gap-1.5">
        <span className="text-dense-micro font-semibold uppercase tracking-wider text-muted-foreground/70">
          Evidence
        </span>
        <Button size="xs" variant="ghost" onClick={onOpenResearchEngine}>
          Research Engine
        </Button>
      </div>
    ) : null

  const activeKey = cycleSteps[activeIndex]?.key
  const stepDetail =
    activeKey === 'pin' ? (
      <p className="m-0 text-dense-meta text-muted-foreground">
        Confirm <code className="font-mono">{tag.trim() || 'semver'}</code> in the registry, then bump{' '}
        <code className="font-mono">k8s/api/deployment.yaml</code> (and MCP / CronJobs if those
        components changed). Agent drafts the pin after the image lands — do not push a missing tag.
      </p>
    ) : activeKey === 'live' ? (
      <p className="m-0 text-dense-meta text-muted-foreground">
        Live:{' '}
        {health?.version != null ? (
          <span className="font-mono">research-api {health.version}</span>
        ) : (
          'health unreachable'
        )}
        {health?.startup_ok === true ? ' · startup_ok' : ''}
        {health?.version === tag.trim() ? ` · matches ${tag.trim()}` : ' · waiting for pin'}
      </p>
    ) : (
      <DeliveryActiveRunPanel target={RESEARCH_TARGET} collapsible />
    )

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      {aiDeploy.error != null && (
        <p className="m-0 text-dense-meta text-destructive">{aiDeploy.error.message}</p>
      )}

      <LaneDetailContextStrip reason={detailReason} />

      <LaneStateStrip
        laneLabel="Satellite · Research"
        actions={
          <div className="flex min-w-0 flex-wrap items-center justify-end gap-x-2 gap-y-1">
            <AgentTriggerButton
              className="shrink-0"
              label={AI_DEPLOY_LABEL}
              pending={aiDeploy.isPending}
              active={agentInFlight}
              activeLabel="Expand dock"
              disabled={!deployDispatchAllowed && !agentInFlight}
              title={
                agentInFlight
                  ? 'Expand Agent Execution Dock — live progress stays on this board'
                  : (deployDisabledReason ?? AI_DEPLOY_LABEL)
              }
              onClick={handleAiDeployClick}
            />
            {evidenceLinks}
          </div>
        }
      >
        <div className="flex flex-wrap items-end gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-dense-micro font-semibold uppercase tracking-wider text-muted-foreground/70">
              Image tag
            </span>
            <input
              value={tag}
              onChange={e => setTag(e.target.value)}
              placeholder={RESEARCH_DEFAULT_TAG}
              className="h-7 w-32 rounded border border-[var(--border)] bg-[var(--card)] px-2 font-mono text-sm"
              aria-label="Image tag"
            />
          </label>
          <span className="text-dense-caption text-muted-foreground">
            Satellite instrument · {RESEARCH_TARGET.pipeline}
          </span>
          {health?.version != null && (
            <DenseTag variant={health.version === tag.trim() ? 'success' : 'neutral'} size="cell">
              live {health.version}
            </DenseTag>
          )}
        </div>
        <p className="m-0 text-dense-caption text-muted-foreground">
          AI Deploy Research starts Tekton with this tag. Pin k8s only after the image lands — Argo
          syncs from GitHub automatically.
        </p>
      </LaneStateStrip>

      <ConstellationStrip
        impact={constellationImpact}
        onFormationLaunch={() =>
          formation.requestLaunch({
            tag: isResearchReleaseTag(tag) ? tag.trim() : undefined,
            revision: 'main',
            env: 'stg',
          })
        }
        formationPending={formation.isPending}
        formationDisabled={!canOperate || deliverInFlight || !isResearchReleaseTag(tag)}
        formationDisabledReason={
          !canOperate
            ? 'Authenticate to launch'
            : deliverInFlight
              ? 'Deliver in flight'
              : !isResearchReleaseTag(tag)
                ? 'Enter a semver image tag'
                : undefined
        }
      />
      {formation.dialog}

      <LaneOperateSplit
        storageKey="bifrost.console.researchLaneOperateSplit"
        primary={
          <>
            <PluginStepCommandCenter
              steps={cycleSteps}
              activeIndex={activeIndex}
              onSelect={setActiveIndex}
              laneLabel="Research"
              idleHint="semver tag · bifrost-deliver-research"
              completeMessage="Research live on the pinned tag"
              revisionHint={isResearchReleaseTag(tag) ? tag.trim() : undefined}
              evidenceSummary="Build → Verify image → Pin manifest → Live. verify-research fail before pin is expected."
              renderStepActions={() => null}
              renderStepDetail={() => stepDetail}
              agentDriven
              aiLaunchLabel={AI_DEPLOY_LABEL}
            />
          </>
        }
        support={
          <>
            <LaneDetailCollapse
              title="Launch checklist"
              summaryExtra={
                <span className="inline-flex flex-wrap items-center gap-2">
                  <DenseTag
                    variant={
                      verdict.kind === 'GO'
                        ? 'success'
                        : verdict.kind === 'IN_FLIGHT'
                          ? 'warning'
                          : 'danger'
                    }
                  >
                    {verdict.kind === 'GO'
                      ? 'GO'
                      : verdict.kind === 'IN_FLIGHT'
                        ? 'IN FLIGHT'
                        : 'NO-GO'}
                  </DenseTag>
                  <DenseTag
                    variant={
                      checkpoints.every(c => c.ok) ? 'success' : 'warning'
                    }
                  >
                    {checkpoints.filter(c => c.ok).length}/{checkpoints.length} ready
                  </DenseTag>
                </span>
              }
              defaultOpen
              showModeBadge
              bodyClassName="flex flex-col gap-3 p-3"
            >
              <p className="m-0 text-dense-meta text-muted-foreground">
                AI Deploy Research stays disabled until auth, pipeline, and a semver tag are green.
                This desk does not start Satellite or Rocket pipelines.
              </p>
              <LaunchGateBar
                layout="column"
                verdict={verdict}
                checkpoints={checkpoints}
                hidePrimaryLaunch
                onExpandAgentDock={onExpandAgentDock}
              />
            </LaneDetailCollapse>

            <LaneDetailCollapse
              title="Research pipeline runs"
              summaryExtra={
                <DenseTag variant="neutral">
                  {runsQ.data?.runs?.length ?? 0} {RESEARCH_TARGET.pipeline}
                </DenseTag>
              }
              defaultOpen
              bodyClassName="p-0"
            >
              <ResearchRunList
                loading={runsQ.isLoading}
                error={runsQ.error instanceof Error ? runsQ.error.message : null}
                runs={runsQ.data?.runs ?? []}
              />
            </LaneDetailCollapse>
          </>
        }
      />
    </div>
  )
}

function ResearchRunList({
  loading,
  error,
  runs,
}: {
  loading: boolean
  error: string | null
  runs: import('@/api/deliveryTypes').DeliveryPipelineRunView[]
}) {
  if (loading) {
    return <p className="m-0 px-3 py-2 text-dense-meta text-muted-foreground">Loading Research runs…</p>
  }
  if (error != null) {
    return <p className="m-0 px-3 py-2 text-dense-meta text-destructive">{error}</p>
  }
  if (runs.length === 0) {
    return (
      <p className="m-0 px-3 py-2 text-dense-meta text-muted-foreground">
        No bifrost-deliver-research runs yet. AI Deploy Research starts the first one.
      </p>
    )
  }
  return (
    <ul className="m-0 flex list-none flex-col divide-y divide-border/50 p-0">
      {runs.slice(0, 8).map(run => {
        const variant = isPipelineRunSucceeded(run)
          ? 'success'
          : isPipelineRunFailed(run)
            ? 'danger'
            : isPipelineRunRunning(run)
              ? 'warning'
              : 'neutral'
        return (
          <li key={run.name} className="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
            <span className="min-w-0 truncate font-mono text-dense-caption">{run.name}</span>
            <span className="inline-flex items-center gap-2">
              <DenseTag variant={variant} size="cell">
                {formatPipelineRunStatus(run)}
              </DenseTag>
              {run.start_time != null && run.start_time !== '' && (
                <span className="font-mono-tabular text-dense-micro text-muted-foreground">
                  {new Date(run.start_time).toLocaleString()}
                </span>
              )}
            </span>
          </li>
        )
      })}
    </ul>
  )
}
