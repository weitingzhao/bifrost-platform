import { useDomainSection } from '@/lib/shell/useHashSection'
import { DomainSection } from '@/components/shell/DomainSection'
import { BackupStatusPanel } from '../data/BackupStatusPanel'
import { RuntimeHosts } from './RuntimeHosts'
import { useQuery } from '@tanstack/react-query'
import { fetchContext } from '@/api/core'
import { ClusterPage } from '@/pages/ClusterPage'
import { NetworkPage } from '@/pages/NetworkPage'
import { MiniCards } from '@/pages/shell/infrastructure/MiniCards'
import { RuntimeMapSection } from '@/pages/shell/infrastructure/RuntimeMapSection'

export function InfrastructurePage() {
  const section = useDomainSection()
  const selected = ['cluster', 'network', 'hosts', 'backup'].includes(section ?? '') ? section : null
  const contextQ = useQuery({
    queryKey: ['ops', 'context', 'infrastructure'],
    queryFn: fetchContext,
    staleTime: 60_000,
  })

  return (
    <div className="flex w-full min-w-0 flex-col gap-6">
      <DomainSection active={selected == null || selected === 'cluster'} id="cluster" label="Cluster">
        <h2 className="m-0 text-sm font-semibold">Cluster</h2>
        <ClusterPage />
      </DomainSection>
      <DomainSection active={selected == null || selected === 'network'} id="network" label="Network">
        <h2 className="m-0 text-sm font-semibold">Network</h2>
        <NetworkPage context={contextQ.data} />
      </DomainSection>
      <DomainSection active={selected == null || selected === 'hosts'} id="hosts" label="Hosts"><RuntimeHosts /><RuntimeMapSection /><MiniCards /></DomainSection>
      <DomainSection active={selected == null || selected === 'backup'} id="backup" label="Storage and backup"><BackupStatusPanel /></DomainSection>
    </div>
  )
}
