import { useQuery } from '@tanstack/react-query'
import { PageHeader } from '@bifrost/ui'
import { useMemo, useState } from 'react'
import { fetchConsoleHosts } from '@/api/console'
import { fetchClusterNodes } from '@/api/platform'
import { ServerTerminal } from '@/components/ServerTerminal'

export function ServerConsolePage() {
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

  const k8sNodeByIp = useMemo(() => {
    const map: Record<string, string> = {}
    for (const node of clusterNodesQuery.data?.nodes ?? []) {
      const ip = node.internal_ip?.trim()
      if (ip !== '') map[ip] = node.name
    }
    return map
  }, [clusterNodesQuery.data?.nodes])

  const hosts = hostsQuery.data ?? []
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const effectiveId = selectedId ?? hosts[0]?.id ?? null

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <PageHeader
        title="Server console"
        description="Interactive SSH via Platform API (topology allowlist). Linux row = K3s cluster nodes (K8s node name + IP); Mac row = native macOS Agent hosts. Open several panes for side-by-side sessions."
      />

      {hostsQuery.isLoading && (
        <p className="text-[var(--muted-foreground)]">Loading SSH hosts…</p>
      )}
      {hostsQuery.isError && (
        <p className="lamp-fail">Failed to load hosts: {(hostsQuery.error as Error).message}</p>
      )}

      <ServerTerminal
        hosts={hosts}
        selectedId={effectiveId}
        onSelectHost={setSelectedId}
        k8sNodeByIp={k8sNodeByIp}
      />
    </div>
  )
}
