import { useQuery } from '@tanstack/react-query'
import { DenseTag } from '@bifrost/ui'
import { Rocket, Satellite } from 'lucide-react'
import { fetchPipelineRuns, fetchSupplyChain } from '@/api/delivery'
import { fetchReleaseGate, fetchStgSmoke } from '@/api/promote'
import {
  gateStepStatus,
  runStepStatus,
  pickDeployPipelineRun,
  deployRunRetryFailed,
} from '@/lib/delivery/releaseStepTypes'
import { buildStgReleasePhases } from '@/lib/architecture/deliveryMainlineCatalog'
import { DELIVER_STG_PIPELINE } from '@/lib/delivery/deliverStgPhases'
import { DELIVER_PLATFORM_PIPELINE } from '@/lib/delivery/deliverPlatformPhases'
import type { OpsContextResponse } from '@/api/opsContextTypes'

/**
 * Release-posture summary strips shared by Mission Launch task strips
 * (OpsTaskStrips) — platform STG mainline, supply chain, and trade STG
 * deliver. Extracted from OpsTaskStrips.tsx to keep that file focused on
 * composition / layout.
 */

export function PlatformStgReleaseStrip({
  onNavigate,
  compact = false,
}: {
  onNavigate: (tab: string) => void
  compact?: boolean
}) {
  const platformRunsQ = useQuery({
    queryKey: ['task-cc', 'platform-runs-summary'],
    queryFn: () => fetchPipelineRuns(DELIVER_PLATFORM_PIPELINE),
    refetchInterval: 20_000,
  })
  const platformStgGateQ = useQuery({
    queryKey: ['task-cc', 'platform-stg-gate-summary'],
    queryFn: () => fetchReleaseGate('platform-stg'),
    refetchInterval: 20_000,
  })
  const supplyQ = useQuery({
    queryKey: ['task-cc', 'supply-chain-summary'],
    queryFn: fetchSupplyChain,
    refetchInterval: 20_000,
  })

  const gate = gateStepStatus(platformStgGateQ.data)
  const runs = platformRunsQ.data?.runs
  const run = pickDeployPipelineRun(runs, {
    gatePassed: platformStgGateQ.data?.result === 'pass',
  })
  const deploy = runStepStatus(run)
  const retryFailed = deployRunRetryFailed(runs, run)
  const cms = supplyQ.data?.dockerfile_configmaps ?? []
  const cmsPresent = cms.filter(c => c.present).length

  if (compact) {
    return (
      <div className="flex flex-col gap-1 border-t border-border/50 pt-1.5">
        <span className="text-[var(--text-dense-micro)] font-medium uppercase tracking-wide text-muted-foreground">
          Last STG deliver
        </span>
        <div className="flex flex-wrap gap-1">
          <DenseTag variant="neutral" className="text-[8px]">
            {deploy.label}
          </DenseTag>
          <DenseTag variant="neutral" className="text-[8px]">
            Gate {gate.label}
          </DenseTag>
          <DenseTag variant="neutral" className="text-[8px]">
            CM {cmsPresent}/{cms.length}
          </DenseTag>
          {retryFailed && (
            <DenseTag variant="neutral" className="text-[8px]">
              retry fail
            </DenseTag>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <Rocket size={16} />
        <span className="text-[var(--text-dense-label)] font-semibold">Platform STG mainline</span>
        <DenseTag variant="neutral" className="text-[9px]">
          Last run
        </DenseTag>
        <DenseTag variant={cmsPresent === cms.length && cms.length > 0 ? 'success' : 'warning'}>
          CMs {cmsPresent}/{cms.length}
        </DenseTag>
      </div>
      <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-muted-foreground">
        {run?.revision != null ? `Revision ${run.revision}` : 'bifrost-deliver-platform pipeline'}
      </p>
      <div className="mt-2 flex flex-wrap gap-2">
        <DenseTag variant={deploy.status === 'done' ? 'success' : deploy.status === 'error' ? 'warning' : 'warning'}>
          Deploy · {deploy.label}
        </DenseTag>
        {retryFailed && (
          <DenseTag variant="neutral" className="text-[9px]">
            Latest retry failed
          </DenseTag>
        )}
        <DenseTag variant={gate.status === 'done' ? 'success' : 'warning'}>Gate · {gate.label}</DenseTag>
      </div>
      <button
        type="button"
        className="mt-2 text-[var(--text-dense-meta)] text-primary hover:underline"
        onClick={() => onNavigate('platform-release')}
      >
        Launch Rocket →
      </button>
    </div>
  )
}

export function SupplyChainStrip({ onNavigate }: { onNavigate: (tab: string) => void }) {
  const supplyQ = useQuery({
    queryKey: ['task-cc', 'supply-chain'],
    queryFn: fetchSupplyChain,
    refetchInterval: 20_000,
  })
  const cms = supplyQ.data?.dockerfile_configmaps ?? []
  const present = cms.filter(c => c.present).length

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex items-center gap-2">
        <Rocket size={16} />
        <span className="text-[var(--text-dense-label)] font-semibold">Platform supply chain</span>
        <DenseTag variant={present === cms.length && cms.length > 0 ? 'success' : 'warning'}>
          CMs {present}/{cms.length}
        </DenseTag>
      </div>
      <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-muted-foreground">
        Mirrors {supplyQ.data?.mirror_credentials_configured ? 'configured' : 'check credentials'}
      </p>
      <button
        type="button"
        className="mt-2 text-[var(--text-dense-meta)] text-primary hover:underline"
        onClick={() => onNavigate('platform-release')}
      >
        Launch Rocket →
      </button>
    </div>
  )
}

export function StgReleaseStrip({
  context,
  onNavigate,
  compact = false,
}: {
  context?: OpsContextResponse
  onNavigate: (tab: string) => void
  compact?: boolean
}) {
  const phases = buildStgReleasePhases(context)
  const active = phases.find(p => p.status === 'active') ?? phases.find(p => p.status === 'blocked')
  const done = phases.filter(p => p.status === 'done').length

  const tradeRunsQ = useQuery({
    queryKey: ['task-cc', 'trade-runs'],
    queryFn: () => fetchPipelineRuns(DELIVER_STG_PIPELINE),
    refetchInterval: 20_000,
  })
  const tradeGateQ = useQuery({
    queryKey: ['task-cc', 'trade-gate'],
    queryFn: () => fetchReleaseGate('stg'),
    refetchInterval: 20_000,
  })
  const smokeQ = useQuery({
    queryKey: ['task-cc', 'stg-smoke'],
    queryFn: fetchStgSmoke,
    refetchInterval: 20_000,
  })

  const gate = gateStepStatus(tradeGateQ.data)
  const smokeOk = smokeQ.data?.reachability === 'ok'
  const runs = tradeRunsQ.data?.runs
  const run = pickDeployPipelineRun(runs, {
    gatePassed: tradeGateQ.data?.result === 'pass',
    smokeOk,
  })
  const deploy = runStepStatus(run)
  const retryFailed = deployRunRetryFailed(runs, run)

  if (compact) {
    return (
      <div className="flex flex-col gap-1 border-t border-border/50 pt-1.5">
        <span className="text-[var(--text-dense-micro)] font-medium uppercase tracking-wide text-muted-foreground">
          Last STG deliver
        </span>
        <div className="flex flex-wrap gap-1">
          <DenseTag variant="neutral" className="text-[8px]">
            {deploy.label}
          </DenseTag>
          <DenseTag variant="neutral" className="text-[8px]">
            Gate {gate.label}
          </DenseTag>
          <DenseTag variant="neutral" className="text-[8px]">
            Smoke {smokeOk ? 'ok' : smokeQ.isLoading ? '…' : 'fail'}
          </DenseTag>
          {retryFailed && (
            <DenseTag variant="neutral" className="text-[8px]">
              retry fail
            </DenseTag>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <Satellite size={16} />
        <span className="text-[var(--text-dense-label)] font-semibold">Trade STG deliver</span>
        <DenseTag variant="neutral" className="text-[9px]">
          Last run
        </DenseTag>
        <DenseTag variant="neutral">
          {done}/{phases.length} phases
        </DenseTag>
      </div>
      <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-muted-foreground">
        bifrost-deliver-stg · smoke + gate (pre-prod checkpoint)
      </p>
      <p className="m-0 mt-0.5 text-[var(--text-dense-meta)]">
        {active != null ? `${active.title} · ${active.status}` : 'All phases complete or planned'}
      </p>
      <div className="mt-2 flex flex-wrap gap-2">
        <DenseTag variant={deploy.status === 'done' ? 'success' : deploy.status === 'error' ? 'warning' : 'warning'}>
          Deploy · {deploy.label}
        </DenseTag>
        {retryFailed && (
          <DenseTag variant="neutral" className="text-[9px]">
            Latest retry failed
          </DenseTag>
        )}
        <DenseTag variant={gate.status === 'done' ? 'success' : 'warning'}>Gate · {gate.label}</DenseTag>
        <DenseTag variant={smokeOk ? 'success' : 'warning'}>
          Smoke · {smokeOk ? 'pass' : smokeQ.isLoading ? '…' : 'fail'}
        </DenseTag>
      </div>
      <button
        type="button"
        className="mt-2 text-[var(--text-dense-meta)] text-primary hover:underline"
        onClick={() => onNavigate('trade-release')}
      >
        Deploy Satellite →
      </button>
    </div>
  )
}
