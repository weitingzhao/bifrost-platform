import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { Button, DenseTag } from '@bifrost/ui'
import { ChevronRight, Rocket, Satellite } from 'lucide-react'
import {
  fetchMatrix,
  fetchPipelineRuns,
  fetchReleaseGate,
  fetchReleaseState,
  fetchStgSmoke,
  fetchSupplyChain,
  fetchTierBStatus,
  isAllMatrices,
} from '@/api/platform'
import type { MatrixResponse } from '@/api/types'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { gateStepStatus, runStepStatus, pickDeployPipelineRun } from '@/components/delivery/ReleaseStepCommandCenter'
import {
  useRocketProdReadiness,
  useSatelliteProdReadiness,
} from '@/components/task-mode/TaskModeReadinessStrip'
import { countsTowardTradeReadiness } from '@/lib/control-room/matrixSummary'
import {
  DELIVER_PLATFORM_PIPELINE,
} from '@/lib/delivery/deliverPlatformPhases'
import { DELIVER_STG_PIPELINE } from '@/lib/delivery/deliverStgPhases'
import { signalColor, type Signal } from '@/lib/control-room/missionSignals'
import { StatusLamp } from '@/components/StatusLamp'

const REFETCH_MS = 20_000

/** Control Room shows both; OpsTaskStrips playbook path may pass one variant. */
export type LaunchPadVariant = 'both' | 'rocket-launch' | 'satellite-deploy'

function tradeEnvSignal(matrix: MatrixResponse | undefined): Signal {
  if (matrix == null) return 'unknown'
  const scored = matrix.targets.filter(countsTowardTradeReadiness)
  const fails = scored.filter(t => t.reachability === 'fail').length
  const degraded = scored.filter(t => t.reachability === 'degraded').length
  if (fails > 0) return 'fail'
  if (degraded > 0) return 'degraded'
  return 'ok'
}

function EnvDot({ label, signal }: { label: string; signal: Signal }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-[var(--text-dense-caption)]">
      <StatusLamp value={signal} kind="reach" />
      <span className="uppercase tracking-wide">{label}</span>
    </span>
  )
}

interface LaunchPadCardProps {
  icon: typeof Rocket
  title: string
  signal: Signal
  summary: string
  detail: string
  tags?: ReactNode
  agentLabel: string
  onAgentLaunch: () => void
  agentPending?: boolean
  canAgentLaunch?: boolean
  agentDisabledReason?: string
  onOpenDetail: () => void
  detailLabel?: string
}

function LaunchPadCard({
  icon: Icon,
  title,
  signal,
  summary,
  detail,
  tags,
  agentLabel,
  onAgentLaunch,
  agentPending = false,
  canAgentLaunch = false,
  agentDisabledReason,
  onOpenDetail,
  detailLabel = 'Open detail →',
}: LaunchPadCardProps) {
  return (
    <div className="launch-pad-card rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex items-start gap-2.5">
        <Icon size={18} className="mt-0.5 shrink-0" style={{ color: signalColor(signal) }} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-[var(--text-dense-label)] font-semibold">{title}</span>
            <StatusLamp value={signal} kind="reach" />
          </div>
          <p className="m-0 mt-0.5 text-[var(--text-dense-meta)] font-medium">{summary}</p>
          <p className="m-0 mt-0.5 text-[var(--text-dense-caption)] text-muted-foreground">{detail}</p>
          {tags != null && <div className="mt-2 flex flex-wrap items-center gap-2">{tags}</div>}
        </div>
      </div>
      <div className="mt-2.5 flex flex-wrap items-center gap-2">
        <AgentTriggerButton
          label={agentLabel}
          pending={agentPending}
          disabled={!canAgentLaunch}
          title={agentDisabledReason ?? agentLabel}
          onClick={onAgentLaunch}
        />
        <Button variant="ghost" size="xs" className="text-[var(--text-dense-meta)]" onClick={onOpenDetail}>
          {detailLabel}
          <ChevronRight size={12} />
        </Button>
      </div>
    </div>
  )
}

export interface LaunchPadProps {
  /** Default `both` — Control Room. Task CC playbook strips may pass one side. */
  variant?: LaunchPadVariant
  onDispatchRelease: () => void
  onDispatchTradeDeploy: () => void
  releasePending?: boolean
  tradeDeployPending?: boolean
  canDispatchRelease?: boolean
  canDispatchTradeDeploy?: boolean
  releaseDisabledReason?: string
  tradeDeployDisabledReason?: string
  onOpenPlatformRelease: () => void
  onOpenTradeDeploy: () => void
  /** Full width, no max-w cap */
  embedded?: boolean
  /** Parent page already shows prod gate banner */
  suppressProdBlockedFeedback?: boolean
}

export function LaunchPad({
  variant = 'both',
  onDispatchRelease,
  onDispatchTradeDeploy,
  releasePending = false,
  tradeDeployPending = false,
  canDispatchRelease = false,
  canDispatchTradeDeploy = false,
  releaseDisabledReason,
  tradeDeployDisabledReason,
  onOpenPlatformRelease,
  onOpenTradeDeploy,
  embedded = false,
  suppressProdBlockedFeedback = false,
}: LaunchPadProps) {
  const showRocket = variant === 'both' || variant === 'rocket-launch'
  const showSatellite = variant === 'both' || variant === 'satellite-deploy'

  const rocketProd = useRocketProdReadiness(showRocket)
  const satelliteProd = useSatelliteProdReadiness(showSatellite)

  const releaseStateQ = useQuery({
    queryKey: ['launch-pad', 'release-state', 'platform'],
    queryFn: () => fetchReleaseState('platform'),
    refetchInterval: REFETCH_MS,
    enabled: showRocket,
  })
  const platformStgRunsQ = useQuery({
    queryKey: ['launch-pad', 'runs', DELIVER_PLATFORM_PIPELINE],
    queryFn: () => fetchPipelineRuns(DELIVER_PLATFORM_PIPELINE),
    refetchInterval: REFETCH_MS,
    enabled: showRocket,
  })
  const platformStgGateQ = useQuery({
    queryKey: ['launch-pad', 'gate', 'platform-stg'],
    queryFn: () => fetchReleaseGate('platform-stg'),
    refetchInterval: REFETCH_MS,
    enabled: showRocket,
  })

  const tradeStgRunsQ = useQuery({
    queryKey: ['launch-pad', 'runs', DELIVER_STG_PIPELINE],
    queryFn: () => fetchPipelineRuns(DELIVER_STG_PIPELINE),
    refetchInterval: REFETCH_MS,
    enabled: showSatellite,
  })
  const tradeStgGateQ = useQuery({
    queryKey: ['launch-pad', 'gate', 'stg'],
    queryFn: () => fetchReleaseGate('stg'),
    refetchInterval: REFETCH_MS,
    enabled: showSatellite,
  })
  const stgSmokeQ = useQuery({
    queryKey: ['launch-pad', 'stg-smoke'],
    queryFn: fetchStgSmoke,
    refetchInterval: REFETCH_MS,
    enabled: showSatellite,
  })
  const tierBQ = useQuery({
    queryKey: ['launch-pad', 'tier-b'],
    queryFn: fetchTierBStatus,
    refetchInterval: REFETCH_MS,
    enabled: showSatellite,
  })
  const supplyQ = useQuery({
    queryKey: ['launch-pad', 'supply-chain'],
    queryFn: fetchSupplyChain,
    refetchInterval: REFETCH_MS,
    enabled: showSatellite,
  })
  const matrixQ = useQuery({
    queryKey: ['launch-pad', 'matrix'],
    queryFn: () => fetchMatrix(),
    refetchInterval: REFETCH_MS,
    enabled: showSatellite,
  })

  const matrices = matrixQ.data != null && isAllMatrices(matrixQ.data) ? matrixQ.data.matrices : []
  const devMatrix = matrices.find(m => m.environment === 'dev')
  const stgMatrix = matrices.find(m => m.environment === 'stg')
  const prodMatrix = matrices.find(m => m.environment === 'prod')

  const platformRun = pickDeployPipelineRun(platformStgRunsQ.data?.runs, {
    gatePassed: platformStgGateQ.data?.result === 'pass',
  })
  const platformDeploy = runStepStatus(platformRun)
  const platformGate = gateStepStatus(platformStgGateQ.data)
  const releaseSignal: Signal =
    platformDeploy.status === 'error' || platformGate.status === 'error'
      ? 'fail'
      : platformDeploy.status === 'active' || platformGate.status === 'active'
        ? 'degraded'
        : platformGate.status === 'done' && platformDeploy.status === 'done'
          ? 'ok'
          : 'degraded'

  const nextAction = releaseStateQ.data?.next_action
  const rocketSummary =
    nextAction?.label != null
      ? `Last deliver · ${nextAction.label}`
      : `Last deliver · ${platformDeploy.label} · ${platformGate.label}`
  const rocketDetail =
    nextAction?.description ??
    (platformRun?.revision != null ? `Revision ${platformRun.revision}` : 'Platform STG → PROD release')

  const smokeOk = stgSmokeQ.data?.reachability === 'ok'
  const tradeRun = pickDeployPipelineRun(tradeStgRunsQ.data?.runs, {
    gatePassed: tradeStgGateQ.data?.result === 'pass',
    smokeOk,
  })
  const tradeDeploy = runStepStatus(tradeRun)
  const tradeGate = gateStepStatus(tradeStgGateQ.data)
  const smokeSignal: Signal = stgSmokeQ.isLoading ? 'unknown' : smokeOk ? 'ok' : 'fail'
  const tradeSignals: Signal[] = [
    tradeEnvSignal(devMatrix),
    tradeEnvSignal(stgMatrix),
    tradeEnvSignal(prodMatrix),
    smokeSignal,
  ]
  const tradeSignal: Signal = tradeSignals.includes('fail')
    ? 'fail'
    : tradeSignals.includes('degraded')
      ? 'degraded'
      : tradeSignals.every(s => s === 'ok')
        ? 'ok'
        : 'unknown'

  const cmsPresent = supplyQ.data?.dockerfile_configmaps?.filter(cm => cm.present).length ?? 0
  const cmsTotal = supplyQ.data?.dockerfile_configmaps?.length ?? 0
  const tierBSigned = tierBQ.data?.signed_off === true

  const rocketAgentBlocked = rocketProd.prodBlocked
  const satelliteAgentBlocked = satelliteProd.prodBlocked
  const rocketCanLaunch = canDispatchRelease && !rocketAgentBlocked
  const satelliteCanLaunch = canDispatchTradeDeploy && !satelliteAgentBlocked
  const rocketDisabledReason = rocketAgentBlocked
    ? rocketProd.prodDisabledReason
    : releaseDisabledReason
  const satelliteDisabledReason = satelliteAgentBlocked
    ? satelliteProd.prodDisabledReason
    : tradeDeployDisabledReason

  const satelliteSummary = `Last deliver · ${tradeDeploy.label} · Smoke ${
    smokeOk ? 'pass' : stgSmokeQ.isLoading ? '…' : 'fail'
  }`
  const satelliteDetail = `${tradeGate.label} · Supply chain ${cmsPresent}/${cmsTotal} CMs`

  return (
    <section
      className={`launch-pad grid gap-3 ${
        embedded
          ? 'w-full'
          : showRocket && showSatellite
            ? 'sm:grid-cols-2'
            : 'max-w-xl'
      }`}
      aria-label="Launch pad"
    >
      {showRocket && rocketAgentBlocked && !suppressProdBlockedFeedback && (
        <OpsFeedback variant="warning" title="Prod readiness blocked" className="sm:col-span-2">
          Fix Platform Prod environment before release — resolve failing namespaces, self-health probes, or release
          gate checks first.
        </OpsFeedback>
      )}
      {showSatellite && satelliteAgentBlocked && !suppressProdBlockedFeedback && (
        <OpsFeedback variant="warning" title="Prod readiness blocked" className="sm:col-span-2">
          Fix Trade Prod environment before deploy — resolve failing pods, datastore, IB socket, or API reachability
          first.
        </OpsFeedback>
      )}
      {showRocket && (
        <LaunchPadCard
          icon={Rocket}
          title="Rocket Launch"
          signal={releaseSignal}
          summary={rocketSummary}
          detail={rocketDetail}
          tags={
            <>
              <DenseTag variant={platformGate.status === 'done' ? 'success' : 'warning'}>
                Gate · {platformGate.label}
              </DenseTag>
              <DenseTag variant={platformDeploy.status === 'done' ? 'success' : 'neutral'}>
                Deploy · {platformDeploy.label}
              </DenseTag>
            </>
          }
          agentLabel="Agent Launch"
          onAgentLaunch={onDispatchRelease}
          agentPending={releasePending}
          canAgentLaunch={rocketCanLaunch}
          agentDisabledReason={rocketDisabledReason}
          onOpenDetail={onOpenPlatformRelease}
        />
      )}

      {showSatellite && (
        <LaunchPadCard
          icon={Satellite}
          title="Satellite Deploy"
          signal={tradeSignal}
          summary={satelliteSummary}
          detail={satelliteDetail}
          tags={
            <>
              <EnvDot label="Dev" signal={tradeEnvSignal(devMatrix)} />
              <EnvDot label="Stg" signal={tradeEnvSignal(stgMatrix)} />
              <EnvDot label="Prod" signal={tradeEnvSignal(prodMatrix)} />
              <DenseTag variant={tierBSigned ? 'success' : 'warning'}>
                Tier B · {tierBSigned ? 'signed' : 'pending'}
              </DenseTag>
            </>
          }
          agentLabel="Agent Deploy"
          onAgentLaunch={onDispatchTradeDeploy}
          agentPending={tradeDeployPending}
          canAgentLaunch={satelliteCanLaunch}
          agentDisabledReason={satelliteDisabledReason}
          onOpenDetail={onOpenTradeDeploy}
        />
      )}
    </section>
  )
}
