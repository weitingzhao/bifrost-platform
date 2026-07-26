import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchAgentBridge, fetchAgentDeployStatus } from '@/api/agentOps'
import type { RunnerStatus } from '@/api/agentTypes'
import {
  bridgeRunners,
  buildMacAgentRoleByHost,
  type MacAgentHostRole,
} from '@/lib/agent/macHostRole'

export type AgentHostPulse = {
  roleByHost: Record<string, MacAgentHostRole>
  primary: RunnerStatus | undefined
  standby: RunnerStatus | undefined
  primaryOk: boolean
  standbyOk: boolean
  /** True when at least one configured runner reports status !== ok. */
  anyRunnerDown: boolean
  /** True when every configured runner is unreachable / unavailable. */
  allRunnersDown: boolean
  bridgeReady: boolean
  /** Collapsed head meta — e.g. `Host · P✓ S✓` / `Host · P✓ S✗` / `Host ?`. */
  hostMetaShort: string
  hostMetaTitle: string
  /** StatusLamp reach derived from L-1 runners (not ambient Fix session). */
  hostReach: 'ok' | 'degraded' | 'fail' | 'unknown'
  deployRunning: boolean
  deployRole: string | undefined
  /** e.g. `Deploy · standby…` */
  deployMetaShort: string | null
}

function pickRole(runners: RunnerStatus[], role: MacAgentHostRole): RunnerStatus | undefined {
  return runners.find(r => r.role === role)
}

function mark(ok: boolean | null): string {
  if (ok == null) return '?'
  return ok ? '✓' : '✗'
}

/**
 * Lightweight L-1 host pulse for Operator Dock head.
 * Bridge ~60s; deploy status polls ~1s only while a job is running.
 */
export function useAgentHostPulse(): AgentHostPulse {
  const bridgeQuery = useQuery({
    queryKey: ['agent', 'bridge'],
    queryFn: fetchAgentBridge,
    staleTime: 30_000,
    refetchInterval: 60_000,
  })

  const deployQuery = useQuery({
    queryKey: ['agent', 'deploy'],
    queryFn: fetchAgentDeployStatus,
    staleTime: 5_000,
    refetchInterval: query => {
      if (query.state.data?.current?.status === 'running') return 1000
      return false
    },
  })

  return useMemo(() => {
    const bridge = bridgeQuery.data
    const runners = bridgeRunners(bridge)
    const primary = pickRole(runners, 'primary') ?? (runners.length === 1 ? runners[0] : undefined)
    const standby = pickRole(runners, 'standby')
    const primaryOk = primary != null ? primary.status === 'ok' : false
    const standbyOk = standby != null ? standby.status === 'ok' : false
    const configured = runners.filter(r => r.status !== 'not_configured')
    const anyRunnerDown = configured.length > 0 && configured.some(r => r.status !== 'ok')
    const allRunnersDown = configured.length > 0 && configured.every(r => r.status !== 'ok')

    const bridgeReady = bridge != null && !bridgeQuery.isError
    let hostMetaShort = 'Host ?'
    let hostMetaTitle = bridgeQuery.isError
      ? 'Agent bridge unavailable'
      : bridgeQuery.isLoading
        ? 'Loading runner heartbeats…'
        : 'No runners configured'

    if (bridgeReady && runners.length > 0) {
      const pMark = primary != null ? mark(primaryOk) : null
      const sMark = standby != null ? mark(standbyOk) : null
      const bits = [
        pMark != null ? `P${pMark}` : null,
        sMark != null ? `S${sMark}` : null,
      ].filter((b): b is string => b != null)
      hostMetaShort = bits.length > 0 ? `Host · ${bits.join(' ')}` : 'Host ?'
      hostMetaTitle = [
        primary != null ? `Primary ${primary.status}${primary.url ? ` · ${primary.url}` : ''}` : null,
        standby != null ? `Standby ${standby.status}${standby.url ? ` · ${standby.url}` : ''}` : null,
      ]
        .filter((l): l is string => l != null)
        .join('\n')
    }

    const current = deployQuery.data?.current
    const deployRunning = current?.status === 'running'
    const deployRole = current?.role
    const deployMetaShort = deployRunning
      ? `Deploy · ${deployRole ?? 'host'}…`
      : null

    let hostReach: AgentHostPulse['hostReach'] = 'unknown'
    if (bridgeReady && runners.length > 0) {
      if (allRunnersDown) hostReach = 'fail'
      else if (anyRunnerDown) hostReach = 'degraded'
      else hostReach = 'ok'
    } else if (bridgeQuery.isError) {
      hostReach = 'fail'
    }

    return {
      roleByHost: buildMacAgentRoleByHost(bridge),
      primary,
      standby,
      primaryOk,
      standbyOk,
      anyRunnerDown,
      allRunnersDown,
      bridgeReady,
      hostMetaShort,
      hostMetaTitle,
      hostReach,
      deployRunning,
      deployRole,
      deployMetaShort,
    }
  }, [bridgeQuery.data, bridgeQuery.isError, bridgeQuery.isLoading, deployQuery.data])
}
