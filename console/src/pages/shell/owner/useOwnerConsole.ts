import { useQueries, useQuery } from '@tanstack/react-query'
import { text } from '@/api/agentRuntime'
import { fetchRuntimeGoal } from '@/api/agentRuntimeGoals'
import { operatorToken } from '@/api/client'
import { fetchClusterNodes, fetchClusterPostgresBackupStatus, fetchDataFreshness } from '@/api/cluster'
import { fetchDeliveryPipelines, fetchPipelineRuns, fetchReleaseWindow } from '@/api/delivery'
import { fetchCoverageDimensions } from '@/api/marketDataDimensions'
import { fetchMarketStatus, isProxyError } from '@/api/marketDataPlugin'
import { fetchReleaseRecords } from '@/api/releases'
import { fetchOrchestrationStatus, isResearchProxyError } from '@/api/researchEngine'
import { fetchTelemetryAlerts } from '@/api/telemetry'
import { useFlexQueryLiveProbe } from '@/hooks/useFlexQueryLiveProbe'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'
import { useMarketDataLiveProbe } from '@/hooks/useMarketDataLiveProbe'
import type { NeedsYouCount } from '@/pages/shell/needs-you/needsYouModel'
import { useNeedsYou } from '@/pages/shell/needs-you/useNeedsYou'
import {
  RELEASE_RECORD_LIMIT,
  isReleasePipeline,
  releaseWindowView,
  splitReleaseRuns,
} from '@/pages/shell/releases/releaseView'
import { backupTile, copiesTile, marketTile, pluginsTile, researchTile } from './dataTiles'
import {
  countBadge,
  dataBadge,
  ownerVerdict,
  planRows,
  quotaRows,
  splitAlerts,
  workNow,
  type TabBadge,
} from './ownerModel'
import type { OwnerTab } from './ownerTabs'

const errorText = (error: unknown): string | null => (error == null ? null : error instanceof Error ? error.message : 'Request failed')

/**
 * Every read the Owner Console makes. Query keys are the ones the detail pages use, so a page
 * opened after this one reads from the same cache. Release runs are read only while the System
 * block is on screen; everything that feeds a tab badge or the verdict is always read.
 */
export function useOwnerConsole(blocks: { system: boolean; work: boolean }) {
  const now = Date.now()
  const needsYou = useNeedsYou()
  const alertsQ = useQuery({ queryKey: ['telemetry', 'alerts'], queryFn: fetchTelemetryAlerts, refetchInterval: 30_000, retry: false })
  const nodesQ = useQuery({ queryKey: ['cluster', 'nodes'], queryFn: fetchClusterNodes, refetchInterval: 30_000, retry: false })
  const hasToken = operatorToken() !== ''
  const windowQ = useQuery({ queryKey: ['delivery', 'release-window'], queryFn: fetchReleaseWindow, enabled: hasToken, refetchInterval: 15_000 })
  const recordsQ = useQuery({ queryKey: ['releases', 'records', RELEASE_RECORD_LIMIT], queryFn: () => fetchReleaseRecords(RELEASE_RECORD_LIMIT),
    enabled: blocks.system, refetchInterval: 30_000 })
  const pipelinesQ = useQuery({ queryKey: ['delivery', 'pipelines'], queryFn: fetchDeliveryPipelines, enabled: blocks.system, refetchInterval: 15_000 })
  const pipelineNames = (pipelinesQ.data?.pipelines ?? []).map(p => p.name).filter(isReleasePipeline).sort()
  const runQs = useQueries({ queries: pipelineNames.map(name => ({ queryKey: ['delivery', 'runs', name], queryFn: () => fetchPipelineRuns(name),
    enabled: blocks.system, refetchInterval: 15_000 })) })

  const backupQ = useQuery({ queryKey: ['cluster', 'postgres', 'backup-status'], queryFn: fetchClusterPostgresBackupStatus, refetchInterval: 60_000, retry: false })
  const copiesQ = useQuery({ queryKey: ['cluster', 'data-freshness'], queryFn: fetchDataFreshness, refetchInterval: 60_000, retry: false })
  const researchQ = useQuery({ queryKey: ['research-engine-orchestration'], queryFn: fetchOrchestrationStatus, refetchInterval: 60_000, retry: 1 })
  const dimsQ = useQuery({ queryKey: ['market-data', 'coverage', 'dimensions'], queryFn: fetchCoverageDimensions, staleTime: 60_000 })
  const marketQ = useQuery({ queryKey: ['market-data', 'vitals', 'analytics'], queryFn: fetchMarketStatus, refetchInterval: 60_000, retry: 1 })
  const ib = useIbGatewayLiveProbe()
  const massive = useMarketDataLiveProbe()
  const flex = useFlexQueryLiveProbe()

  const runtime = needsYou.runtimeQuery.data
  const board = needsYou.progressQuery.data?.document ?? null
  const work = workNow(runtime)
  const lastId = work?.lastFinished?.mission ?? ''
  const lastGoalQ = useQuery({ queryKey: ['agent-runtime-goal', lastId], queryFn: ({ signal }) => fetchRuntimeGoal(lastId, signal),
    enabled: blocks.work && lastId !== '', retry: false, staleTime: Infinity, refetchOnWindowFocus: false })

  const nodes = nodesQ.data?.nodes ?? []
  const standby = nodes.filter(n => n.elastic_mode === 'standby').map(n => ({ name: n.name, internalIp: n.internal_ip }))
  const core = nodes.filter(n => n.elastic_mode !== 'standby')
  // An empty node list is a read that found nothing (no cluster access), not a cluster with no nodes.
  const nodeCount = nodes.length > 0 ? { ready: core.filter(n => n.status === 'Ready').length, total: core.length, standby: standby.length } : null
  const alerts = alertsQ.data ? splitAlerts(alertsQ.data.alerts, standby, nodes.length, now) : null

  const lane = releaseWindowView({ hasToken, isLoading: windowQ.isLoading, error: errorText(windowQ.error), data: windowQ.data, now })
  const laneText = lane.state === 'free' ? 'release lane free' : lane.state === 'held' ? 'release lane held' : 'release lane unknown'
  const records = recordsQ.data?.records ?? []
  const runs = splitReleaseRuns(pipelineNames.map((pipeline, i) => ({ pipeline, runs: runQs[i]?.data?.runs ?? [] })), records)

  const approvals = needsYou.decide.length
  const ownerAlerts = alerts?.yours.length ?? 0
  // Yours is Needs you without the silent threads: those are the coordinator's to check (Owner 2026-10-15).
  const waiting: NeedsYouCount = needsYou.count.state === 'known' ? { state: 'known', count: needsYou.decisionItems.length } : needsYou.count
  const verdict = ownerVerdict({ waiting, ownerAlerts, approvals, running: work ? work.running : null, lane: laneText,
    nodes: nodeCount ? { ready: nodeCount.ready, total: nodeCount.total } : null })

  const marketItems = marketQ.data != null && !isProxyError(marketQ.data) && marketQ.data.ok !== false ? marketQ.data.freshness_summary ?? [] : undefined
  const marketError = marketQ.isError ? errorText(marketQ.error) : marketQ.data != null && (isProxyError(marketQ.data) || marketQ.data.ok === false) ? 'market-data plugin did not answer' : null
  const session: string | null | undefined = dimsQ.data != null ? (dimsQ.data.session ?? null) : dimsQ.isError ? null : undefined
  const research = researchQ.data != null && !isResearchProxyError(researchQ.data) ? researchQ.data.data : undefined
  const researchError = researchQ.isError ? errorText(researchQ.error) : researchQ.data != null && isResearchProxyError(researchQ.data) ? researchQ.data.error : null
  const tiles = [
    marketTile({ data: marketItems, error: marketError }, session),
    pluginsTile([
      { name: 'IB', reach: ib.probeReach, loading: ib.isLoading },
      { name: 'Massive', reach: massive.probeReach, loading: massive.isLoading },
      { name: 'IB Flex', reach: flex.probeReach, loading: flex.isLoading },
    ]),
    researchTile({ data: research, error: researchError }, now),
    backupTile({ data: backupQ.data, error: errorText(backupQ.error) }, now),
    copiesTile({ data: copiesQ.data, error: errorText(copiesQ.error) }),
  ]

  const yoursCount = waiting.state === 'known' ? waiting.count + ownerAlerts : null
  const badges: Partial<Record<OwnerTab, TabBadge>> = {
    you: needsYou.count.state === 'loading' ? null : countBadge(yoursCount, 'attention'),
    work: work == null ? null : work.stuck > 0 ? countBadge(work.stuck, 'fail') : countBadge(work.running, 'info'),
    system: alerts == null ? null : alerts.critical > 0 ? countBadge(alerts.critical, 'fail') : countBadge(alerts.warning, 'muted'),
    data: countBadge(dataBadge(tiles), 'muted'),
  }

  return {
    now,
    needsYou,
    verdict,
    badges,
    alerts,
    alertsError: errorText(alertsQ.error),
    work,
    lastGoal: text(lastGoalQ.data?.mission.goal) || text(lastGoalQ.data?.mission.title),
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
    runsLoading: blocks.system && (pipelinesQ.isLoading || recordsQ.isLoading),
    tiles,
    updatedAt: Math.max(alertsQ.dataUpdatedAt, nodesQ.dataUpdatedAt, needsYou.query.dataUpdatedAt),
  }
}

export type OwnerConsoleData = ReturnType<typeof useOwnerConsole>
