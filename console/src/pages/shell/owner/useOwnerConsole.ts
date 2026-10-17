import { useQueries, useQuery } from '@tanstack/react-query'
import { text } from '@/api/agentRuntime'
import { fetchRuntimeGoal } from '@/api/agentRuntimeGoals'
import { operatorToken } from '@/api/client'
import { fetchClusterPostgresBackupStatus, fetchDataFreshness } from '@/api/cluster'
import { fetchDeliveryPipelines, fetchPipelineRuns, fetchReleaseWindow } from '@/api/delivery'
import { fetchCoverageDimensions } from '@/api/marketDataDimensions'
import { fetchMarketStatus, isProxyError } from '@/api/marketDataPlugin'
import { fetchReleaseRecords } from '@/api/releases'
import { fetchOrchestrationStatus, isResearchProxyError } from '@/api/researchEngine'
import { useFlexQueryLiveProbe } from '@/hooks/useFlexQueryLiveProbe'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'
import { useMarketDataLiveProbe } from '@/hooks/useMarketDataLiveProbe'
import {
  RELEASE_RECORD_LIMIT,
  isReleasePipeline,
  releaseWindowView,
  splitReleaseRuns,
} from '@/pages/shell/releases/releaseView'
import { backupTile, copiesTile, marketTile, pluginsTile, researchTile, type ProbeReach } from './dataTiles'
import {
  ownerVerdict,
  planRows,
  goalRefreshMs,
  lastResult,
  quotaRows,
  workNow,
} from './ownerModel'
import { current, errorText, useOwnerYours } from './useOwnerYours'

/**
 * Every read the Owner Console makes. Query keys are the ones the detail pages use, so a page
 * opened after this one reads from the same cache. Release runs are read only while System is on
 * screen; everything that feeds the verdict is always read.
 */
export function useOwnerConsole(blocks: { system: boolean; work: boolean; chief: boolean }) {
  const now = Date.now()
  const { needsYou, alertsQ, nodesQ, nodes, standby, alerts, alertsError, standbyEvidence, yours } = useOwnerYours(now)
  const hasToken = operatorToken() !== ''
  const windowQ = useQuery({ queryKey: ['delivery', 'release-window'], queryFn: fetchReleaseWindow, enabled: hasToken, refetchInterval: 15_000 })
  const recordsQ = useQuery({ queryKey: ['releases', 'records', RELEASE_RECORD_LIMIT], queryFn: () => fetchReleaseRecords(RELEASE_RECORD_LIMIT),
    enabled: blocks.system, refetchInterval: 30_000 })
  const pipelinesQ = useQuery({ queryKey: ['delivery', 'pipelines'], queryFn: fetchDeliveryPipelines, enabled: blocks.system, refetchInterval: 15_000 })
  const pipelineNames = (current(pipelinesQ)?.pipelines ?? []).map(p => p.name).filter(isReleasePipeline).sort()
  const runQs = useQueries({ queries: pipelineNames.map(name => ({ queryKey: ['delivery', 'runs', name], queryFn: () => fetchPipelineRuns(name),
    enabled: blocks.system, refetchInterval: 15_000 })) })

  const backupQ = useQuery({ queryKey: ['cluster', 'postgres', 'backup-status'], queryFn: fetchClusterPostgresBackupStatus, refetchInterval: 60_000, retry: false })
  const copiesQ = useQuery({ queryKey: ['cluster', 'data-freshness'], queryFn: fetchDataFreshness, refetchInterval: 60_000, retry: false })
  const researchQ = useQuery({ queryKey: ['research-engine-orchestration'], queryFn: fetchOrchestrationStatus, refetchInterval: 60_000, retry: 1 })
  // The session moves while the page stays open on a phone; read it again every minute (review 221 B6).
  const dimsQ = useQuery({ queryKey: ['market-data', 'coverage', 'dimensions'], queryFn: fetchCoverageDimensions, staleTime: 60_000, refetchInterval: 60_000 })
  const marketQ = useQuery({ queryKey: ['market-data', 'vitals', 'analytics'], queryFn: fetchMarketStatus, refetchInterval: 60_000, retry: 1 })
  const ib = useIbGatewayLiveProbe()
  const massive = useMarketDataLiveProbe()
  const flex = useFlexQueryLiveProbe()

  // A failed refresh voids the last answer: agents idle, nodes ready and so on are not shown from a cache (review 221 A7).
  const runtime = current(needsYou.runtimeQuery)
  const board = needsYou.progressQuery.data?.document ?? null
  const work = workNow(runtime)
  const lastId = work?.lastAccepted?.mission ?? ''
  const lastGoalQ = useQuery({ queryKey: ['agent-runtime-goal', lastId], queryFn: ({ signal }) => fetchRuntimeGoal(lastId, signal),
    enabled: (blocks.work || blocks.chief) && lastId !== '', retry: false, staleTime: Infinity, refetchOnWindowFocus: false,
    refetchInterval: q => goalRefreshMs(text(q.state.data?.mission.state)) })

  const core = nodes.filter(n => n.elastic_mode !== 'standby')
  // An empty node list is a read that found nothing (no cluster access), not a cluster with no nodes.
  const nodeCount = nodes.length > 0 ? { ready: core.filter(n => n.status === 'Ready').length, total: core.length, standby: standby.length } : null


  const lane = releaseWindowView({ hasToken, isLoading: windowQ.isLoading, error: errorText(windowQ.error), data: current(windowQ), now })
  const laneText = lane.state === 'free' ? 'release lane free' : lane.state === 'held' ? 'release lane held' : 'release lane unknown'
  const records = current(recordsQ)?.records ?? []
  const runs = splitReleaseRuns(pipelineNames.map((pipeline, i) => ({ pipeline, runs: current(runQs[i])?.runs ?? [] })), records)
  // "Nothing delivering" needs every run list read now: an error, an offline pause or no answer yet is unread (review 221 A6, 222 A-R3).
  const runSources = [pipelinesQ, recordsQ, ...runQs]
  const runsRead = blocks.system && runSources.every(q => q.data !== undefined && !q.isError && !q.isPaused)
  const runsError = runSources.map(q => (q.isError ? errorText(q.error) : q.isPaused ? 'offline' : null)).find(e => e != null) ?? null
  const runsLoading = blocks.system && !runsRead && runsError == null

  const verdict = ownerVerdict({ yours, approvals: needsYou.openCount.state === 'known' ? needsYou.decide.length : null,
    running: work ? work.running : null, lane: laneText, nodes: nodeCount ? { ready: nodeCount.ready, total: nodeCount.total } : null })

  const marketData = current(marketQ)
  const marketItems = marketData != null && !isProxyError(marketData) && marketData.ok !== false ? marketData.freshness_summary ?? [] : undefined
  const marketError = marketQ.isError ? errorText(marketQ.error) : marketData != null && (isProxyError(marketData) || marketData.ok === false) ? 'market-data plugin did not answer' : null
  const dims = current(dimsQ)
  const session: string | null | undefined = dimsQ.isError ? null : dims != null ? (dims.session ?? null) : undefined
  const researchData = current(researchQ)
  const research = researchData != null && !isResearchProxyError(researchData) ? researchData.data : undefined
  const researchError = researchQ.isError ? errorText(researchQ.error) : researchData != null && isResearchProxyError(researchData) ? researchData.error : null
  const reach = (p: { probeReach: ProbeReach; readFailed: boolean }): ProbeReach => (p.readFailed ? 'unknown' : p.probeReach)
  const tiles = [
    marketTile({ data: marketItems, error: marketError }, session),
    pluginsTile([
      { name: 'IB', reach: reach(ib), loading: ib.isLoading },
      { name: 'Massive', reach: reach(massive), loading: massive.isLoading },
      { name: 'IB Flex', reach: reach(flex), loading: flex.isLoading },
    ]),
    researchTile({ data: research, error: researchError }, now),
    backupTile({ data: current(backupQ), error: errorText(backupQ.error) }, now),
    copiesTile({ data: current(copiesQ), error: errorText(copiesQ.error) }),
  ]

  return {
    now,
    needsYou,
    yours,
    verdict,
    alerts,
    alertsError,
    standbyEvidence,
    work,
    runtimeError: needsYou.runtimeQuery.isError,
    lastGoal: text(lastGoalQ.data?.mission.goal) || text(lastGoalQ.data?.mission.title),
    last: lastResult(work, current(lastGoalQ) ? text(lastGoalQ.data?.mission.state) : ''),
    board,
    boardStale: needsYou.progressQuery.data?.stale === true,
    plan: planRows(board?.roadmap?.round),
    arch: board?.roadmap?.arch?.rows ?? [],
    quota: quotaRows(board?.usage?.pools, now),
    hosts: runtime?.hosts ?? [],
    nodes,
    nodeCount,
    nodesError: errorText(nodesQ.error),
    lane,
    inFlight: runs.inProgress,
    records: records.slice(0, 4),
    runsRead,
    runsLoading,
    runsError,
    tiles,
    updatedAt: Math.max(alertsQ.dataUpdatedAt, nodesQ.dataUpdatedAt, needsYou.query.dataUpdatedAt),
  }
}

export type OwnerConsoleData = ReturnType<typeof useOwnerConsole>
