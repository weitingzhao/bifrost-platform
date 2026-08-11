import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { cn, DenseTag, StatusLamp } from '@bifrost/ui'
import { AlertTriangle, ChevronRight, Rocket, type LucideIcon } from 'lucide-react'
import { fetchCluster, fetchClusterServiceReadiness } from '@/api/cluster'
import { fetchReleaseGate } from '@/api/promote'
import { fetchSelfHealth } from '@/api/core'
import { SignalActivityObserver } from '@/components/activity/SignalActivityObserver'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { ReadinessFixBar } from '@/components/task-mode/ReadinessFixBar'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import {
  primaryChipNavigation,
  setSatelliteApiEnv,
  setSatelliteBusFocus,
  setSatelliteHealthSection,
  type ReadinessChipContext,
} from '@/lib/task-mode/readinessChipActions'
import { readinessAnchorDomId } from '@/lib/task-mode/satelliteLaunchVerdict'
import {
  infraSignal,
  missionStatus,
  missionStatusColor,
  worst,
  type Signal,
} from '@/lib/control-room/missionSignals'
import {
  DELIVER_STG_RECOVER_SCOPE,
  PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
} from '@/lib/agent/agentScopes'
import { PROD_ENV_FIX_SCOPE } from '@/lib/agent/prodEnvironmentFixPrompt'
import { SATELLITE_BUS_INGEST_TRIAGE_SCOPE } from '@/lib/agent/satelliteBusIngestTriagePrompt'
import { usePromoteVerifyReadiness, useRocketProdReadiness } from './hooks'
import {
  isProdReleaseBlocked,
  namespacePods,
  PLATFORM_PROD,
  PLATFORM_STG,
  REFETCH_MS,
  releaseGateSignal,
  selfHealthEnvSignal,
  stripOverallTag,
  type EnvChip,
} from './utils'

type ReadinessChipProps = {
  label: string
  signal: Signal
  detail: string
  onDrillDown?: () => void
  title?: string
}

export function ReadinessChip({ label, signal, detail, onDrillDown, title }: ReadinessChipProps) {
  const failing = signal !== 'ok'
  const inner = (
    <>
      <div className="flex items-center gap-1.5">
        <StatusLamp value={signal} kind="reach" />
        <span className="min-w-0 flex-1 truncate text-[var(--text-dense-meta)] font-medium">{label}</span>
        {onDrillDown != null && (
          <ChevronRight
            size={12}
            className={cn('shrink-0', failing ? 'text-warning' : 'text-muted-foreground')}
            aria-hidden
          />
        )}
      </div>
      <p className="m-0 mt-0.5 truncate text-[var(--text-dense-caption)] text-muted-foreground">{detail}</p>
    </>
  )
  if (onDrillDown == null) {
    return (
      <div className="rounded border border-border/60 bg-card px-2 py-1.5" title={title}>
        {inner}
      </div>
    )
  }
  return (
    <button
      type="button"
      className={cn(
        'w-full rounded border bg-card px-2 py-1.5 text-left transition-colors hover:border-primary/40 hover:bg-primary/5',
        failing ? 'border-warning/40' : 'border-border/60',
      )}
      title={title ?? `Open details: ${label}`}
      onClick={onDrillDown}
    >
      {inner}
    </button>
  )
}

type EnvironmentReadinessPanelProps = {
  title: string
  icon: LucideIcon
  overall: Signal
  isLoading: boolean
  chips: EnvChip[]
  compact?: boolean
  summaryColumn?: boolean
  /** Scroll target from LaunchGateBar checkpoint (1:1 lamp ↔ panel). */
  readinessAnchor?: 'rocket' | 'trade-prod' | 'platform-prod' | 'stg'
  linkLabel: string
  onLink: () => void
  onNavigate: (tabId: string) => void
  fixCtx?: ReadinessChipContext
  canOperate?: boolean
  onAgentFix?: () => void
  agentFixPending?: boolean
  agentFixDisabled?: boolean
  agentFixTitle?: string
}

export function EnvironmentReadinessPanel({
  title,
  icon: Icon,
  overall,
  isLoading,
  chips,
  compact = false,
  summaryColumn = false,
  readinessAnchor,
  linkLabel,
  onLink,
  onNavigate,
  fixCtx,
  canOperate = false,
  onAgentFix,
  agentFixPending,
  agentFixDisabled,
  agentFixTitle,
}: EnvironmentReadinessPanelProps) {
  const tag = stripOverallTag(overall, isLoading)
  const color = missionStatusColor(missionStatus(overall))
  const dense = compact || summaryColumn

  return (
    <div
      id={readinessAnchor != null ? readinessAnchorDomId(readinessAnchor) : undefined}
      className={cn(
        'rounded-lg border border-border bg-secondary scroll-mt-2 transition-shadow',
        dense ? 'px-2.5 py-2' : 'px-3 py-2.5',
        summaryColumn && 'flex h-full min-h-0 flex-col',
      )}
    >
      <SignalActivityObserver
        chips={chips}
        envScope={fixCtx?.activityEnvScope ?? fixCtx?.env ?? readinessAnchor ?? 'unknown'}
      />
      <div className="flex flex-wrap items-center gap-1.5">
        <Icon size={dense ? 14 : 16} style={{ color }} />
        <span className={cn('font-semibold', dense ? 'text-[var(--text-dense-meta)]' : 'text-[var(--text-dense-label)]')}>
          {title}
        </span>
        <StatusLamp value={overall} kind="reach" />
        <DenseTag variant={tag.variant} className={dense ? 'text-[9px]' : undefined}>
          {tag.label}
        </DenseTag>
      </div>
      <div
        className={cn(
          'mt-1.5 grid gap-1.5',
          // Mission Launch summary: one chip per row (STG/Prod already side-by-side).
          summaryColumn ? 'grid-cols-1' : dense ? 'grid-cols-2' : 'sm:grid-cols-2',
        )}
      >
        {chips.map(chip => {
          const nav = fixCtx != null ? primaryChipNavigation(chip.label, fixCtx) : null
          return (
            <ReadinessChip
              key={chip.label}
              label={chip.label}
              signal={chip.signal}
              detail={chip.detail}
              onDrillDown={
                nav != null
                  ? () => {
                      if (nav.tabId === 'satellite-bus') {
                        setSatelliteBusFocus(nav.busFocus)
                      }
                      if (
                        nav.tabId === 'satellite-health' ||
                        nav.tabId === 'satellite-api' ||
                        nav.tabId === 'satellite-telemetry'
                      ) {
                        setSatelliteApiEnv(nav.apiEnv)
                        setSatelliteHealthSection(
                          nav.healthSection ??
                            (nav.tabId === 'satellite-telemetry' ? 'runtime' : 'probes'),
                        )
                      }
                      onNavigate(
                        nav.tabId === 'satellite-api' || nav.tabId === 'satellite-telemetry'
                          ? 'satellite-health'
                          : nav.tabId,
                      )
                    }
                  : undefined
              }
            />
          )
        })}
      </div>
      {fixCtx != null && (
        <ReadinessFixBar
          chips={chips}
          ctx={fixCtx}
          canOperate={canOperate}
          onNavigate={onNavigate}
          onAgentFix={onAgentFix}
          agentFixPending={agentFixPending}
          agentFixDisabled={agentFixDisabled}
          agentFixTitle={agentFixTitle}
          dense={dense}
        />
      )}
      {!summaryColumn && (
        <button
          type="button"
          className="mt-2 text-[var(--text-dense-meta)] text-primary hover:underline"
          onClick={onLink}
        >
          {linkLabel}
        </button>
      )}
    </div>
  )
}

export function ProdBlockedBanner({ context }: { context: 'satellite' | 'rocket' }) {
  const copy =
    context === 'satellite'
      ? 'Satellite deploy and promote require a healthy Trade Prod stack. Resolve Prod failures before deploying or releasing.'
      : 'Platform release requires healthy Prod namespaces and gates. Resolve Prod failures before launching release agents.'
  return (
    <OpsFeedback variant="warning" title="Fix Prod environment before release">
      <span className="inline-flex items-start gap-1.5">
        <AlertTriangle size={14} className="mt-0.5 shrink-0" aria-hidden />
        {copy}
      </span>
    </OpsFeedback>
  )
}

export function RocketReadinessStrip({
  compact = false,
  summaryColumn = false,
  suppressProdBlockedBanner = false,
  onNavigate,
  canOperate = false,
  onAgentFix,
  agentFixPending = false,
  agentFixDisabled = false,
  agentFixTitle,
}: {
  compact?: boolean
  summaryColumn?: boolean
  suppressProdBlockedBanner?: boolean
  onNavigate: (tabId: string) => void
  canOperate?: boolean
  /** Platform Prod / STG readiness Agent Fix (same ambient task as LaunchGateBar). */
  onAgentFix?: () => void
  agentFixPending?: boolean
  agentFixDisabled?: boolean
  agentFixTitle?: string
}) {
  const { snapshot, isLoading: missionLoading } = useMissionSnapshot()
  const { prodOverall, isLoading: prodLoading } = useRocketProdReadiness()

  const serviceQ = useQuery({
    queryKey: ['task-cc', 'service-readiness'],
    queryFn: fetchClusterServiceReadiness,
    refetchInterval: REFETCH_MS,
  })

  const clusterQ = useQuery({
    queryKey: ['task-cc', 'cluster'],
    queryFn: fetchCluster,
    refetchInterval: REFETCH_MS,
  })

  const selfQ = useQuery({
    queryKey: ['cockpit', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: REFETCH_MS,
  })

  const stgGateQ = useQuery({
    queryKey: ['task-cc', 'platform-stg-gate'],
    queryFn: () => fetchReleaseGate('platform-stg'),
    refetchInterval: REFETCH_MS,
  })

  const prodGateQ = useQuery({
    queryKey: ['task-cc', 'platform-prod-gate'],
    queryFn: () => fetchReleaseGate('platform-prod'),
    refetchInterval: REFETCH_MS,
  })

  const cicdDomain = serviceQ.data?.domains.find(d => d.id === 'cicd')
  const cicdSignal = (cicdDomain?.reachability ?? 'unknown') as Signal
  const cicdDetail = cicdDomain?.summary ?? serviceQ.data?.detail ?? 'Tekton · platform namespaces'

  const cluster = clusterQ.data
  const k8sStg = useMemo(() => {
    const infra = infraSignal(cluster)
    const ns = namespacePods(cluster, PLATFORM_STG)
    return { signal: worst(infra.signal, ns.signal), detail: `${infra.detail} · ${ns.detail}` }
  }, [cluster])

  const k8sProd = useMemo(() => namespacePods(cluster, PLATFORM_PROD), [cluster])
  const selfStg = useMemo(() => selfHealthEnvSignal(selfQ.data?.probes, 'stg'), [selfQ.data?.probes])
  const selfProd = useMemo(() => selfHealthEnvSignal(selfQ.data?.probes, 'prod'), [selfQ.data?.probes])
  const stgGate = useMemo(() => releaseGateSignal(stgGateQ.data), [stgGateQ.data])
  const prodGate = useMemo(() => releaseGateSignal(prodGateQ.data), [prodGateQ.data])
  const promoteVerify = usePromoteVerifyReadiness()

  const stgOverall = worst(k8sStg.signal, cicdSignal, selfStg.signal, stgGate.signal, snapshot.release.signal)
  const prodOverallLocal = worst(
    k8sProd.signal,
    selfProd.signal,
    prodGate.signal,
    snapshot.release.signal,
    promoteVerify.promoteSignal,
  )

  const stgLoading =
    missionLoading ||
    serviceQ.isLoading ||
    clusterQ.isLoading ||
    selfQ.isLoading ||
    stgGateQ.isLoading
  const prodPanelLoading =
    prodLoading ||
    clusterQ.isLoading ||
    selfQ.isLoading ||
    prodGateQ.isLoading ||
    promoteVerify.isLoading

  const showProdBanner =
    !suppressProdBlockedBanner &&
    (isProdReleaseBlocked(prodOverallLocal) || isProdReleaseBlocked(prodOverall))

  const stgChips: EnvChip[] = summaryColumn
    ? [
        { label: 'Rocket · K8s STG', signal: k8sStg.signal, detail: k8sStg.detail, fixScope: PROD_ENV_FIX_SCOPE },
        { label: 'CI/CD', signal: cicdSignal, detail: cicdDetail, fixScope: DELIVER_STG_RECOVER_SCOPE },
        {
          label: 'Self-health STG',
          signal: selfStg.signal,
          detail: selfStg.detail,
          fixScope: PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
        },
        { label: 'STG gate', signal: stgGate.signal, detail: stgGate.detail, fixScope: DELIVER_STG_RECOVER_SCOPE },
      ]
    : [
        { label: 'Rocket · K8s STG', signal: k8sStg.signal, detail: k8sStg.detail, fixScope: PROD_ENV_FIX_SCOPE },
        { label: 'CI/CD', signal: cicdSignal, detail: cicdDetail, fixScope: DELIVER_STG_RECOVER_SCOPE },
        {
          label: 'Self-health STG',
          signal: selfStg.signal,
          detail: selfStg.detail,
          fixScope: PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
        },
        {
          label: 'STG release gate',
          signal: stgGate.signal,
          detail: stgGate.detail,
          fixScope: DELIVER_STG_RECOVER_SCOPE,
        },
        {
          label: 'Supply chain',
          signal: snapshot.release.signal,
          detail: snapshot.release.detail,
          fixScope: DELIVER_STG_RECOVER_SCOPE,
        },
      ]

  const prodChips: EnvChip[] = [
    { label: 'Rocket · K8s PROD', signal: k8sProd.signal, detail: k8sProd.detail, fixScope: PROD_ENV_FIX_SCOPE },
    {
      label: 'Self-health PROD',
      signal: selfProd.signal,
      detail: selfProd.detail,
      fixScope: PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
    },
    { label: 'PROD gate', signal: prodGate.signal, detail: prodGate.detail, fixScope: DELIVER_STG_RECOVER_SCOPE },
    {
      label: 'Supply chain',
      signal: snapshot.release.signal,
      detail: snapshot.release.detail,
      fixScope: DELIVER_STG_RECOVER_SCOPE,
    },
    {
      label: 'Promote / cutover',
      signal: promoteVerify.promoteSignal,
      detail: promoteVerify.promoteDetail,
      fixScope: DELIVER_STG_RECOVER_SCOPE,
    },
  ]

  return (
    <div className={cn('flex min-h-0 flex-col', summaryColumn ? 'h-full gap-1.5' : 'gap-2')}>
      {showProdBanner && <ProdBlockedBanner context="rocket" />}
      <div
        className={cn(
          'grid min-h-0 gap-2',
          summaryColumn ? 'flex-1 md:grid-cols-2 [&>*]:h-full' : undefined,
        )}
      >
        <EnvironmentReadinessPanel
          title={summaryColumn ? 'STG platform' : 'Platform STG readiness'}
          icon={Rocket}
          overall={stgOverall}
          isLoading={stgLoading}
          compact={compact}
          summaryColumn={summaryColumn}
          readinessAnchor="stg"
          chips={stgChips}
          linkLabel="Launch Rocket →"
          onLink={() => onNavigate('platform-release')}
          onNavigate={onNavigate}
          fixCtx={{ modeId: 'ops', env: 'platform-stg' }}
          canOperate={canOperate}
          onAgentFix={onAgentFix}
          agentFixPending={agentFixPending}
          agentFixDisabled={agentFixDisabled}
          agentFixTitle={agentFixTitle}
        />
        <EnvironmentReadinessPanel
          title={summaryColumn ? 'Platform Prod' : 'PROD environment readiness'}
          icon={Rocket}
          overall={prodOverallLocal}
          isLoading={prodPanelLoading}
          compact={compact}
          summaryColumn={summaryColumn}
          readinessAnchor="platform-prod"
          chips={prodChips}
          linkLabel="Launch Rocket →"
          onLink={() => onNavigate('platform-release')}
          onNavigate={onNavigate}
          fixCtx={{ modeId: 'ops', env: 'platform-prod' }}
          canOperate={canOperate}
          onAgentFix={onAgentFix}
          agentFixPending={agentFixPending}
          agentFixDisabled={agentFixDisabled}
          agentFixTitle={agentFixTitle}
        />
      </div>
    </div>
  )
}

export function SharedRocketStrip({
  rocket,
  isLoading,
  compact = false,
  onNavigate,
  canOperate = false,
}: {
  rocket: { signal: Signal; detail: string }
  isLoading: boolean
  compact?: boolean
  onNavigate: (tabId: string) => void
  canOperate?: boolean
}) {
  const tag = stripOverallTag(rocket.signal, isLoading)
  const chips: EnvChip[] = [
    {
      label: 'Rocket · IB socket',
      signal: rocket.signal,
      detail: rocket.detail,
      fixScope: SATELLITE_BUS_INGEST_TRIAGE_SCOPE,
    },
  ]
  const openBus = () => {
    setSatelliteBusFocus('rocket')
    onNavigate('satellite-bus')
  }

  return (
    <div
      id={readinessAnchorDomId('rocket')}
      className={cn(
        'rounded-lg border border-border bg-secondary scroll-mt-2 transition-shadow',
        compact ? 'px-2.5 py-2' : 'px-3 py-2.5',
      )}
    >
      <div className="flex flex-wrap items-center gap-1.5">
        <Rocket size={compact ? 14 : 16} className="text-muted-foreground" />
        <span
          className={cn(
            'font-semibold',
            compact ? 'text-[var(--text-dense-meta)]' : 'text-[var(--text-dense-label)]',
          )}
        >
          Shared · IB bus
        </span>
        <StatusLamp value={rocket.signal} kind="reach" />
        <DenseTag variant={tag.variant} className={compact ? 'text-[9px]' : undefined}>
          {tag.label}
        </DenseTag>
        <DenseTag variant="neutral" className="text-[9px]">
          COUPLING
        </DenseTag>
      </div>
      <SignalActivityObserver chips={chips} envScope="shared" />
      <div className="mt-1.5 grid gap-1.5 grid-cols-1">
        <ReadinessChip
          label="Rocket · IB socket"
          signal={rocket.signal}
          detail={rocket.detail}
          onDrillDown={openBus}
        />
      </div>
      <ReadinessFixBar
        chips={chips}
        ctx={{ modeId: 'ops', env: 'prod', activityEnvScope: 'shared' }}
        canOperate={canOperate}
        onNavigate={onNavigate}
        dense={compact}
      />
      <button
        type="button"
        className="mt-1.5 text-[var(--text-dense-caption)] text-primary hover:underline"
        onClick={openBus}
      >
        Bus Status → Rocket
      </button>
    </div>
  )
}
