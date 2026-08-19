import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchPipelineRuns } from '@/api/delivery'
import { fetchAgentBridge, fetchAgentDeployStatus } from '@/api/agentOps'
import {
  usePromoteVerifyReadiness,
  useRocketProdReadiness,
  useSatelliteProdReadiness,
} from '@/components/task-mode/readiness/hooks'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  isAmbientAgentActive,
  type AmbientAgentJob,
} from '@/lib/agent/ambientAgent'
import { PLUGIN_LAUNCH_SCOPE } from '@/lib/agent/pluginLaunchAgentPrompt'
import { AGENT_LAUNCH_SCOPE } from '@/lib/agent/agentLaunchAgentPrompt'
import { PLATFORM_RELEASE_SCOPE } from '@/lib/agent/platformReleaseAgentPrompt'
import { TRADE_DEPLOY_SCOPE } from '@/lib/agent/tradeDeployAgentPrompt'
import { missionStatus, type Signal } from '@/lib/control-room/missionSignals'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'
import {
  readPluginLaunchEvidence,
  readPluginLaunchStore,
} from '@/lib/delivery/pluginLaunchEvidence'
import {
  AGENT_LAUNCH_STORE_EVENT,
  agentLaunchEvidenceKey,
  isAgentLaunchLastDeployOk,
  readAgentLaunchEvidence,
  readAgentLaunchStore,
} from '@/lib/delivery/agentLaunchEvidence'
import {
  buildPluginLaunchCheckpoints,
  resolvePluginLaunchVerdict,
} from '@/lib/task-mode/pluginLaunchVerdict'
import {
  buildLaunchCheckpoints,
  hasDeliverInFlight,
  launchVerdictToSignal,
  resolveLaunchVerdict,
  type LaunchVerdict,
} from '@/lib/task-mode/satelliteLaunchVerdict'

const PLATFORM_STG_PIPELINE = deliveryTargetById('platform-stg').pipeline
const PLATFORM_PROD_PIPELINE = deliveryTargetById('platform-prod').pipeline
const TRADE_STG_PIPELINE = deliveryTargetById('trade-stg').pipeline
const TRADE_PROD_PIPELINE = deliveryTargetById('trade-prod').pipeline

export type LaunchDeskLaneId = 'platform-release' | 'trade-release' | 'plugin-release' | 'agent-release'

export type LaunchDeskLaneSignal = {
  signal: Signal
  title: string
  verdict: LaunchVerdict
  checklistReady: number
  checklistTotal: number
}

function verdictTitle(
  lane: string,
  verdict: LaunchVerdict,
  ready: number,
  total: number,
): string {
  const gate =
    verdict.kind === 'GO'
      ? 'GO — ready to publish'
      : verdict.kind === 'IN_FLIGHT'
        ? 'IN FLIGHT'
        : 'NO-GO — checklist blocked'
  return `${lane} checklist: ${gate} (${ready}/${total}) · ${verdict.detail}`
}

/**
 * Live Launch Desk icon lamps — same checklist verdicts as lane pages / Mission Launch TCC.
 * GO = green (can publish), NO-GO = red, IN_FLIGHT = yellow.
 */
export function useLaunchDeskChecklistSignals(opts?: {
  ambientJobId?: string | null
  ambientJobStatus?: AmbientAgentJob['status'] | null
  ambientJobScope?: string | null
  enabled?: boolean
}): Record<LaunchDeskLaneId, LaunchDeskLaneSignal> {
  const enabled = opts?.enabled !== false
  const { canOperate } = usePlatformAuth()
  const [agentEvidenceTick, setAgentEvidenceTick] = useState(0)
  useEffect(() => {
    const bump = () => setAgentEvidenceTick(n => n + 1)
    window.addEventListener(AGENT_LAUNCH_STORE_EVENT, bump)
    window.addEventListener('storage', bump)
    return () => {
      window.removeEventListener(AGENT_LAUNCH_STORE_EVENT, bump)
      window.removeEventListener('storage', bump)
    }
  }, [])
  const rocketProd = useRocketProdReadiness(enabled)
  const satelliteProd = useSatelliteProdReadiness(enabled)
  const promoteVerify = usePromoteVerifyReadiness(enabled)
  const ibProbe = useIbGatewayLiveProbe()

  const platformStgRuns = useQuery({
    queryKey: ['delivery', 'runs', PLATFORM_STG_PIPELINE],
    queryFn: () => fetchPipelineRuns(PLATFORM_STG_PIPELINE),
    refetchInterval: 20_000,
    enabled,
  })
  const platformProdRuns = useQuery({
    queryKey: ['delivery', 'runs', PLATFORM_PROD_PIPELINE],
    queryFn: () => fetchPipelineRuns(PLATFORM_PROD_PIPELINE),
    refetchInterval: 20_000,
    enabled,
  })
  const tradeStgRuns = useQuery({
    queryKey: ['delivery', 'runs', TRADE_STG_PIPELINE],
    queryFn: () => fetchPipelineRuns(TRADE_STG_PIPELINE),
    refetchInterval: 20_000,
    enabled,
  })
  const tradeProdRuns = useQuery({
    queryKey: ['delivery', 'runs', TRADE_PROD_PIPELINE],
    queryFn: () => fetchPipelineRuns(TRADE_PROD_PIPELINE),
    refetchInterval: 20_000,
    enabled,
  })

  const ambientActive = isAmbientAgentActive(opts?.ambientJobId, opts?.ambientJobStatus)
  const scope = opts?.ambientJobScope ?? null

  const agentBridge = useQuery({
    queryKey: ['agent', 'bridge'],
    queryFn: fetchAgentBridge,
    refetchInterval: 60_000,
    enabled,
  })
  const agentDeploy = useQuery({
    queryKey: ['agent', 'deploy'],
    queryFn: fetchAgentDeployStatus,
    refetchInterval: q => {
      if (q.state.data?.current?.status === 'running') return 2000
      return 60_000
    },
    enabled,
  })

  return useMemo(() => {
    void agentEvidenceTick
    const rocketInput = {
      mode: 'rocket' as const,
      canOperate,
      prodBlocked: rocketProd.prodBlocked,
      tradeProdLabel: missionStatus(rocketProd.prodOverall),
      tradeProdSignal: rocketProd.prodOverall,
      promoteSignal: promoteVerify.promoteSignal,
      promoteDetail: promoteVerify.promoteDetail,
      deliverInFlight:
        hasDeliverInFlight(platformStgRuns.data?.runs) ||
        hasDeliverInFlight(platformProdRuns.data?.runs),
      agentInFlight: ambientActive && scope === PLATFORM_RELEASE_SCOPE,
    }
    const rocketVerdict = resolveLaunchVerdict(rocketInput)
    const rocketCps = buildLaunchCheckpoints(rocketInput)

    const satelliteInput = {
      mode: 'satellite' as const,
      canOperate,
      prodBlocked: satelliteProd.prodBlocked,
      blockKind: satelliteProd.blockKind ?? undefined,
      rocketLabel: missionStatus(satelliteProd.rocketSignal),
      rocketDetail: satelliteProd.rocketDetail,
      tradeProdLabel: missionStatus(satelliteProd.tradeProdOverall),
      tradeProdSignal: satelliteProd.tradeProdOverall,
      rocketSignal: satelliteProd.rocketSignal,
      promoteSignal: promoteVerify.promoteSignal,
      promoteDetail: promoteVerify.promoteDetail,
      deliverInFlight:
        hasDeliverInFlight(tradeStgRuns.data?.runs) ||
        hasDeliverInFlight(tradeProdRuns.data?.runs),
      agentInFlight: ambientActive && scope === TRADE_DEPLOY_SCOPE,
    }
    const satelliteVerdict = resolveLaunchVerdict(satelliteInput)
    const satelliteCps = buildLaunchCheckpoints(satelliteInput)

    const store = readPluginLaunchStore()
    const evidence = readPluginLaunchEvidence(store.selectedTarget, store.selectedSeat)
    const pluginAgentInFlight = ambientActive && scope === PLUGIN_LAUNCH_SCOPE
    const pluginVerdict = resolvePluginLaunchVerdict({
      canOperate,
      target: store.selectedTarget,
      status: ibProbe.status,
      evidence,
      agentInFlight: pluginAgentInFlight,
    })
    const pluginCps = buildPluginLaunchCheckpoints({
      canOperate,
      target: store.selectedTarget,
      status: ibProbe.status,
      evidence,
      agentInFlight: pluginAgentInFlight,
    })

    const rocketLoading = rocketProd.isLoading || promoteVerify.isLoading
    const satelliteLoading = satelliteProd.isLoading || promoteVerify.isLoading
    const pluginLoading = ibProbe.isLoading

    const rocketReady = rocketCps.filter(c => c.ok).length
    const satelliteReady = satelliteCps.filter(c => c.ok).length
    const pluginReady = pluginCps.filter(c => c.ok).length

    return {
      'platform-release': {
        signal: rocketLoading ? 'unknown' : launchVerdictToSignal(rocketVerdict.kind),
        title: verdictTitle('Rocket', rocketVerdict, rocketReady, rocketCps.length),
        verdict: rocketVerdict,
        checklistReady: rocketReady,
        checklistTotal: rocketCps.length,
      },
      'trade-release': {
        signal: satelliteLoading ? 'unknown' : launchVerdictToSignal(satelliteVerdict.kind),
        title: verdictTitle('Satellite', satelliteVerdict, satelliteReady, satelliteCps.length),
        verdict: satelliteVerdict,
        checklistReady: satelliteReady,
        checklistTotal: satelliteCps.length,
      },
      'plugin-release': {
        signal: pluginLoading ? 'unknown' : launchVerdictToSignal(pluginVerdict.kind),
        title: verdictTitle('Plugin', pluginVerdict, pluginReady, pluginCps.length),
        verdict: pluginVerdict,
        checklistReady: pluginReady,
        checklistTotal: pluginCps.length,
      },
      'agent-release': (() => {
        const agentInFlight =
          agentDeploy.data?.current?.status === 'running' ||
          (ambientActive && scope === AGENT_LAUNCH_SCOPE)
        const store = readAgentLaunchStore()
        const evidenceKey = agentLaunchEvidenceKey(store.selectedTarget)
        const runners = agentBridge.data?.runners ?? []
        const targetRunner =
          runners.find(r => r.role === evidenceKey) ??
          runners[0] ??
          (agentBridge.data?.remediation_runner != null
            ? agentBridge.data.remediation_runner
            : undefined)
        const runnersOk = targetRunner?.status === 'ok'
        const deployEnabled = agentDeploy.data?.enabled === true
        // Same Last-deploy rule as Launch Agent page (both → primary evidence key).
        const lastOk = isAgentLaunchLastDeployOk(
          agentDeploy.data?.last?.status,
          readAgentLaunchEvidence(store.selectedTarget),
        )
        const agentLoading = agentBridge.isLoading || agentDeploy.isLoading
        const agentChecks = [canOperate, deployEnabled, runnersOk, lastOk]
        const agentReady = agentChecks.filter(Boolean).length
        const agentTotal = agentChecks.length
        let agentVerdictKind: LaunchVerdict['kind']
        if (agentInFlight) agentVerdictKind = 'IN_FLIGHT'
        else if (agentReady === agentTotal) agentVerdictKind = 'GO'
        else agentVerdictKind = 'NO_GO'
        const agentVerdict: LaunchVerdict = { kind: agentVerdictKind, title: `Agent ${agentVerdictKind}`, detail: `${agentReady}/${agentTotal} ready` }
        return {
          signal: agentLoading ? ('unknown' as Signal) : launchVerdictToSignal(agentVerdictKind),
          title: verdictTitle('Agent', agentVerdict, agentReady, agentTotal),
          verdict: agentVerdict,
          checklistReady: agentReady,
          checklistTotal: agentTotal,
        }
      })(),
    }
  }, [
    canOperate,
    rocketProd.prodBlocked,
    rocketProd.prodOverall,
    rocketProd.isLoading,
    satelliteProd.prodBlocked,
    satelliteProd.blockKind,
    satelliteProd.rocketSignal,
    satelliteProd.rocketDetail,
    satelliteProd.tradeProdOverall,
    satelliteProd.isLoading,
    promoteVerify.promoteSignal,
    promoteVerify.promoteDetail,
    promoteVerify.isLoading,
    platformStgRuns.data?.runs,
    platformProdRuns.data?.runs,
    tradeStgRuns.data?.runs,
    tradeProdRuns.data?.runs,
    ambientActive,
    scope,
    ibProbe.status,
    ibProbe.isLoading,
    agentBridge.data,
    agentBridge.isLoading,
    agentDeploy.data,
    agentDeploy.isLoading,
    agentEvidenceTick,
  ])
}
