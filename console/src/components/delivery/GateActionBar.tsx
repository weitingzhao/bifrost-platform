import { Button } from '@bifrost/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { fetchPipelineRuns } from '@/api/delivery'
import { fetchReleaseGate, runReleaseGate, type ReleaseGateTier } from '@/api/promote'
import { fetchSelfHealth } from '@/api/core'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import type { DeliveryTargetConfig } from '@/lib/delivery/deliveryTargets'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'
import { buildGateDebugBundle } from '@/lib/promote/buildGateDebugBundle'

type CopyState = 'idle' | 'copied' | 'error'

function targetForGateTier(tier: ReleaseGateTier): DeliveryTargetConfig {
  switch (tier) {
    case 'stg':
      return deliveryTargetById('trade-stg')
    case 'prod':
      return deliveryTargetById('trade-prod')
    case 'platform-stg':
      return deliveryTargetById('platform-stg')
    case 'platform-prod':
      return deliveryTargetById('platform-prod')
  }
}

interface GateActionBarProps {
  tier: ReleaseGateTier
  label: string
}

export function GateActionBar({ tier, label }: GateActionBarProps) {
  const { canAdmin } = usePlatformAuth()
  const qc = useQueryClient()
  const target = targetForGateTier(tier)

  const gateQuery = useQuery({
    queryKey: ['promote', 'release-gate', tier],
    queryFn: () => fetchReleaseGate(tier),
    refetchInterval: 30_000,
  })
  const selfHealthQuery = useQuery({
    queryKey: ['platform', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: 30_000,
  })
  const runsQuery = useQuery({
    queryKey: ['delivery', 'runs', target.pipeline],
    queryFn: () => fetchPipelineRuns(target.pipeline),
    refetchInterval: 15_000,
  })

  const gate = gateQuery.data
  const result = gate?.result ?? ''
  const failed = result === 'fail' || (gate?.blockers?.length ?? 0) > 0

  const [runError, setRunError] = useState<string | null>(null)
  const mutation = useMutation({
    mutationFn: () => runReleaseGate(tier),
    onMutate: () => setRunError(null),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['promote', 'release-gate', tier] })
      void qc.invalidateQueries({ queryKey: ['promote', 'gate-history'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'self-health'] })
    },
    onError: (err: Error) => setRunError(err.message),
  })

  const [copyState, setCopyState] = useState<CopyState>('idle')

  const buildBundle = () =>
    buildGateDebugBundle({
      tier,
      label,
      pipeline: target.pipeline,
      namespace: target.namespace,
      gate,
      runs: runsQuery.data,
      selfHealth: selfHealthQuery.data,
    })

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
    a.download = `gate-debug-${tier}-${Date.now()}.md`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="flex flex-col gap-2">
      {failed && (
        <span className="text-dense-caption text-destructive font-medium">
          Gate failed — {(gate?.blockers?.length ?? 0)} blocker{(gate?.blockers?.length ?? 0) > 1 ? 's' : ''}
        </span>
      )}
      <div className="flex flex-wrap items-center gap-3">
        {canAdmin && (
          <Button
            disabled={mutation.isPending}
            onClick={() => mutation.mutate()}
            className="shadow-sm"
          >
            {mutation.isPending ? 'Running…' : `Run ${label} Gate`}
          </Button>
        )}
        {failed && (
          <>
            <Button size="sm" variant="outline" onClick={() => void handleAskAi()}>
              {copyState === 'copied'
                ? 'Copied — paste into AI'
                : copyState === 'error'
                  ? 'Copy failed'
                  : 'Ask AI for Help'}
            </Button>
            <Button size="sm" variant="ghost" onClick={handleDownload}>
              Download log
            </Button>
          </>
        )}
        {!canAdmin && !failed && (
          <span className="text-dense-caption text-muted-foreground">Authenticate as admin to run gate.</span>
        )}
      </div>
      {copyState === 'copied' && (
        <p className="m-0 text-dense-caption text-success">
          Debug bundle copied — paste it into your AI assistant to diagnose the failure.
        </p>
      )}
      {runError && <p className="m-0 text-dense-caption text-destructive">{runError}</p>}
    </div>
  )
}
