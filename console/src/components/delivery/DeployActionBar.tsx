import { Button } from '@bifrost/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { fetchPipelineRuns, fetchPipelineRunSteps, fetchRevisions, fetchSupplyChain, startPipelineRun } from '@/api/delivery'
import { fetchReleaseState } from '@/api/promote'
import { fetchSelfHealth } from '@/api/core'
import { RevisionPicker } from '@/components/delivery/RevisionPicker'
import { isRevisionDeployReady } from '@/components/delivery/revisionPickerUtils'
import { RefPreflightStatus } from '@/components/delivery/RefPreflightPanel'
import { isRefDeployBlocked, useRefPreflight } from '@/components/delivery/useRefPreflight'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { buildDeployDebugBundle } from '@/lib/delivery/buildDeployDebugBundle'
import { deliveryFocusRunQueryKey } from '@/lib/delivery/deliveryFocusRun'
import type { DeliveryTargetConfig } from '@/lib/delivery/deliveryTargets'
import { isPipelineRunFailed } from '@/lib/delivery/pipelineRunAskPack'

type CopyState = 'idle' | 'copied' | 'error'

interface DeployActionBarProps {
  target: DeliveryTargetConfig
  /** Release state tier query key segment — 'platform' or 'trade' */
  releaseStateTier?: string
  deployButtonLabel?: string
  /** Ambient agent / remediation job id — persisted on the release cycle. */
  agentSessionId?: string | null
}

export function DeployActionBar({
  target,
  releaseStateTier = 'platform',
  deployButtonLabel,
  agentSessionId,
}: DeployActionBarProps) {
  const { canOperate } = usePlatformAuth()
  const qc = useQueryClient()
  const [revision, setRevision] = useState('main')
  const [actionError, setActionError] = useState<string | null>(null)
  const [actionSuccess, setActionSuccess] = useState<string | null>(null)

  const supplyQuery = useQuery({
    queryKey: ['delivery', 'supply-chain'],
    queryFn: fetchSupplyChain,
    refetchInterval: 15_000,
  })
  const cmPresent = (name: string) =>
    supplyQuery.data?.dockerfile_configmaps?.some(cm => cm.name === name && cm.present) ?? false
  const cmAllOk = target.dockerfileConfigMaps.every(exp => cmPresent(exp.name))

  const revisionsQuery = useQuery({
    queryKey: ['delivery', 'revisions', target.mirrorRepos],
    queryFn: () => fetchRevisions(target.mirrorRepos),
    staleTime: 60_000,
  })

  const runsQuery = useQuery({
    queryKey: ['delivery', 'runs', target.pipeline],
    queryFn: () => fetchPipelineRuns(target.pipeline),
    refetchInterval: 15_000,
  })
  const selfHealthQuery = useQuery({
    queryKey: ['platform', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: 30_000,
  })
  const releaseStateQuery = useQuery({
    queryKey: ['promote', 'release-state', releaseStateTier],
    queryFn: () => fetchReleaseState(releaseStateTier),
    refetchInterval: 30_000,
  })

  const latestRun = runsQuery.data?.runs?.[0]
  const latestRunFailed = latestRun != null && isPipelineRunFailed(latestRun)

  const latestRunStepsQuery = useQuery({
    queryKey: ['delivery', 'steps', latestRun?.name, latestRun?.namespace],
    queryFn: () => fetchPipelineRunSteps(latestRun!.name, latestRun!.namespace),
    enabled: latestRunFailed && latestRun != null,
    staleTime: 30_000,
  })

  const hasError = !!actionError || latestRunFailed

  const deliverMutation = useMutation({
    mutationFn: (rev: string) =>
      startPipelineRun(target.pipeline, rev, agentSessionId ?? undefined),
    onMutate: () => {
      setActionError(null)
      setActionSuccess(null)
    },
    onSuccess: data => {
      const runName = data.run?.name
      setActionSuccess(
        runName
          ? `PipelineRun ${runName} started — watch progress below.`
          : 'Pipeline run started.',
      )
      if (runName) {
        qc.setQueryData(deliveryFocusRunQueryKey(target.pipeline), runName)
        void qc.invalidateQueries({ queryKey: ['delivery', 'steps', runName] })
      }
      void qc.invalidateQueries({ queryKey: ['delivery', 'runs', target.pipeline] })
      void qc.invalidateQueries({ queryKey: ['delivery', 'supply-chain'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => setActionError(err.message),
  })

  const [copyState, setCopyState] = useState<CopyState>('idle')

  const buildBundle = () => {
    const effectiveRevision = latestRun?.revision?.trim() || revision.trim()
    return buildDeployDebugBundle({
      target: target.shortLabel,
      pipeline: target.pipeline,
      namespace: target.namespace,
      revision: effectiveRevision,
      actionError,
      run: latestRun,
      runs: runsQuery.data,
      steps: latestRunStepsQuery.data,
      supplyChain: supplyQuery.data,
      releaseState: releaseStateQuery.data,
      selfHealth: selfHealthQuery.data,
    })
  }

  const handleAskAi = async () => {
    try {
      await navigator.clipboard.writeText(buildBundle())
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2500)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 2500)
    }
  }

  const handleDownload = () => {
    const blob = new Blob([buildBundle()], { type: 'text/markdown' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `deploy-debug-${target.shortLabel.toLowerCase()}-${Date.now()}.md`
    a.click()
    URL.revokeObjectURL(url)
  }

  const refPreflight = useRefPreflight(target.pipeline, revision)
  const deployBlockedByRef = isRefDeployBlocked(refPreflight.data)
  const buttonLabel = deployButtonLabel ?? `Deploy to ${target.shortLabel}`

  return (
    <div className="flex flex-col gap-2">
      {!cmAllOk && (
        <span className="text-dense-caption text-warning">⚠ Dockerfile ConfigMaps not ready</span>
      )}
      {canOperate ? (
        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-start gap-3">
            <span className="text-dense-caption text-muted-foreground shrink-0 pt-2">Revision</span>
            <RevisionPicker
              value={revision}
              onChange={setRevision}
              revisions={revisionsQuery.data}
              isLoading={revisionsQuery.isLoading}
              repoLabels={target.mirrorRepos}
            />
            <Button
              disabled={
                deliverMutation.isPending
                || !cmAllOk
                || !isRevisionDeployReady(revision)
                || deployBlockedByRef
              }
              onClick={() => deliverMutation.mutate(revision.trim())}
              className="shadow-sm mt-0.5"
            >
              {deliverMutation.isPending ? 'Starting…' : buttonLabel}
            </Button>
            {hasError && (
              <>
                <Button size="sm" variant="outline" onClick={() => void handleAskAi()}>
                  {copyState === 'copied'
                    ? 'Copied — paste into AI'
                    : copyState === 'error'
                      ? 'Copy failed'
                      : 'Issue for AI'}
                </Button>
                <Button size="sm" variant="ghost" onClick={handleDownload}>
                  Download log
                </Button>
              </>
            )}
          </div>
          {target.mirrorRepos.length > 1 && (
            <RefPreflightStatus
              data={refPreflight.data}
              isLoading={refPreflight.isLoading}
              revision={revision}
            />
          )}
        </div>
      ) : (
        <span className="text-dense-caption text-muted-foreground">Authenticate as operator to deploy.</span>
      )}
      {actionError && <p className="m-0 text-dense-caption text-destructive">{actionError}</p>}
      {actionSuccess && <p className="m-0 text-dense-caption text-success">{actionSuccess}</p>}
      {copyState === 'copied' && (
        <p className="m-0 text-dense-caption text-success">
          Debug bundle copied — paste it into your AI assistant to diagnose the failure.
        </p>
      )}
    </div>
  )
}
