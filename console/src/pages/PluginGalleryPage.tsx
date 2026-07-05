import { DenseTag, PageHeader } from '@bifrost/ui'
import { IbGatewayCutoverStatusPanel } from '@/components/cluster/IbGatewayCutoverStatusPanel'
import { IbGatewayLiveStatusPanel } from '@/components/cluster/IbGatewayLiveStatusPanel'
import { OpsSection } from '@/components/layout/OpsSection'

const PLUGIN_REGISTRY = [
  {
    id: 'ib-gateway',
    name: 'IB Gateway',
    vendor: 'Interactive Brokers',
    role: 'TWS socket bridge · redis-ib @ data NS',
    status: 'live',
  },
  {
    id: 'massive-stock',
    name: 'Massive Stock feed',
    vendor: 'Polygon.io',
    role: 'Planned subcontractor plugin',
    status: 'planned',
  },
  {
    id: 'massive-option',
    name: 'Massive Option feed',
    vendor: 'Polygon.io',
    role: 'Planned subcontractor plugin',
    status: 'planned',
  },
  {
    id: 'flex-query',
    name: 'IB Flex Query',
    vendor: 'Interactive Brokers',
    role: 'Planned subcontractor plugin',
    status: 'planned',
  },
] as const

function statusVariant(status: string): 'success' | 'neutral' | 'info' {
  if (status === 'live') return 'success'
  if (status === 'planned') return 'neutral'
  return 'info'
}

export function PluginGalleryPage() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <PageHeader
        title="Plugin Gallery"
        description="External subcontractor plugins — live L0 probes and L1 actuation for platform-managed integrations."
      />

      <OpsSection title="Plugin registry" bodyPadding="default" overflow="visible">
        <div className="grid gap-2 sm:grid-cols-2">
          {PLUGIN_REGISTRY.map(plugin => (
            <div
              key={plugin.id}
              className="rounded-md border border-[var(--border)] bg-[var(--secondary)] px-3 py-2"
            >
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-[var(--text-dense-label)] font-semibold">{plugin.name}</span>
                <DenseTag variant={statusVariant(plugin.status)}>{plugin.status}</DenseTag>
              </div>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                {plugin.vendor} · {plugin.role}
              </p>
            </div>
          ))}
        </div>
      </OpsSection>

      <section className="page-section flex flex-col gap-2" aria-label="IB Gateway live">
        <div className="px-3 pt-2">
          <h2 className="m-0 text-sm font-semibold">IB Gateway — live status</h2>
          <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Shared infrastructure plugin — redis-ib @ data NS · mode actuation and Trade cutover probes.
          </p>
        </div>
        <IbGatewayLiveStatusPanel />
        <IbGatewayCutoverStatusPanel />
      </section>
    </div>
  )
}
