import { useQuery } from '@tanstack/react-query'
import { PageHeader } from '@bifrost/ui'
import { useState } from 'react'
import { fetchConsoleHosts } from '@/api/console'
import { ServerTerminal } from '@/components/ServerTerminal'

export function ServerConsolePage() {
  const hostsQuery = useQuery({
    queryKey: ['console-hosts'],
    queryFn: fetchConsoleHosts,
    refetchInterval: 30_000,
  })

  const hosts = hostsQuery.data ?? []
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const effectiveId = selectedId ?? hosts[0]?.id ?? null

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <PageHeader
        title="Server console"
        description="Interactive SSH via Platform API (topology allowlist). Select a host to connect; open several for a side-by-side grid. Retry appears if a session fails. Passphrase keys use macOS ssh-agent — run ssh-add first if needed."
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
      />
    </div>
  )
}
