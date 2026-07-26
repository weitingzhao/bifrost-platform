import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { cn } from '@bifrost/ui'
import { fetchAgentBridge } from '@/api/agentOps'
import { fetchConsoleHosts } from '@/api/console'
import { fetchClusterNodes } from '@/api/cluster'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import {
  OpsVerdictStrip,
  type OpsVerdictLamp,
  type OpsVerdictTagVariant,
} from '@/components/layout/OpsVerdictStrip'
import { ServerTerminal } from '@/components/ServerTerminal'
import { buildMacAgentRoleByHost } from '@/lib/agent/macHostRole'

export type ServerConsolePanelProps = {
  /** Page shell shows OpsVerdictStrip; Operator Dock omits it (framework head owns status). */
  showVerdict?: boolean
  /** Tighter panes for dock body. */
  density?: 'page' | 'dock'
  className?: string
}

/**
 * Shared Server Console surface — Operator Dock Console slot (shell framework).
 */
export function ServerConsolePanel({
  showVerdict = false,
  density = 'dock',
  className,
}: ServerConsolePanelProps) {
  const hostsQuery = useQuery({
    queryKey: ['console-hosts'],
    queryFn: fetchConsoleHosts,
    refetchInterval: 30_000,
  })

  const clusterNodesQuery = useQuery({
    queryKey: ['cluster', 'nodes'],
    queryFn: fetchClusterNodes,
    staleTime: 60_000,
    refetchInterval: 60_000,
  })

  /** Shared with Operator Dock / Plane — tags Mac chips Primary/Standby from runners. */
  const bridgeQuery = useQuery({
    queryKey: ['agent', 'bridge'],
    queryFn: fetchAgentBridge,
    staleTime: 30_000,
    refetchInterval: 60_000,
  })

  const k8sNodeByIp = useMemo(() => {
    const map: Record<string, string> = {}
    for (const node of clusterNodesQuery.data?.nodes ?? []) {
      const ip = node.internal_ip?.trim()
      if (ip !== '') map[ip] = node.name
    }
    return map
  }, [clusterNodesQuery.data?.nodes])

  const agentRoleByHost = useMemo(
    () => buildMacAgentRoleByHost(bridgeQuery.data),
    [bridgeQuery.data],
  )

  const hosts = hostsQuery.data ?? []
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const effectiveId = selectedId ?? hosts[0]?.id ?? null
  const selectedHost = hosts.find(h => h.id === effectiveId) ?? hosts[0] ?? null

  const reachableCount = useMemo(() => hosts.filter(h => h.reachable).length, [hosts])
  const totalCount = hosts.length
  const hostsSummary = `${reachableCount}/${totalCount} hosts reachable`

  let verdictLamp: OpsVerdictLamp = 'ok'
  let verdictTag = 'READY'
  let verdictTagVariant: OpsVerdictTagVariant = 'success'
  let verdictSummary = hostsSummary

  if (hostsQuery.isLoading) {
    verdictLamp = 'unknown'
    verdictTag = 'LOADING'
    verdictTagVariant = 'neutral'
    verdictSummary = 'Loading SSH hosts…'
  } else if (hostsQuery.isError) {
    verdictLamp = 'fail'
    verdictTag = 'ERROR'
    verdictTagVariant = 'danger'
    verdictSummary =
      hostsQuery.error instanceof Error
        ? hostsQuery.error.message
        : 'Failed to load SSH hosts.'
  } else if (totalCount === 0) {
    verdictLamp = 'degraded'
    verdictTag = 'NO HOSTS'
    verdictTagVariant = 'warning'
    verdictSummary = hostsSummary
  } else if (selectedHost != null && !selectedHost.reachable) {
    verdictLamp = 'fail'
    verdictTag = 'UNREACHABLE'
    verdictTagVariant = 'danger'
  } else {
    verdictLamp = 'ok'
    verdictTag = 'READY'
    verdictTagVariant = 'success'
  }

  return (
    <div
      className={
        className ??
        cn(
          'flex min-h-0 w-full min-w-0 flex-1 flex-col',
          density === 'dock' ? 'gap-0' : 'gap-2',
        )
      }
    >
      {showVerdict ? (
        <OpsVerdictStrip
          ariaLabel="Server console freshness"
          title="SERVER CONSOLE"
          lamp={verdictLamp}
          tagLabel={verdictTag}
          tagVariant={verdictTagVariant}
          summary={verdictSummary}
        />
      ) : null}

      {hostsQuery.isError ? (
        <OpsFeedback variant="error" title="Failed to load SSH hosts">
          {hostsQuery.error instanceof Error
            ? hostsQuery.error.message
            : 'Console hosts request failed.'}
        </OpsFeedback>
      ) : null}

      <ServerTerminal
        hosts={hosts}
        selectedId={effectiveId}
        onSelectHost={setSelectedId}
        k8sNodeByIp={k8sNodeByIp}
        agentRoleByHost={agentRoleByHost}
        density={density}
      />
    </div>
  )
}
