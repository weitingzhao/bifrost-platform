import { IbGatewayCutoverStatusPanel } from '@/components/cluster/IbGatewayCutoverStatusPanel'
import { IbGatewayLiveStatusPanel } from '@/components/cluster/IbGatewayLiveStatusPanel'

/** Live L0 probes + L1 actuation for platform plugins deployed in cluster (data NS). */
export function ClusterPlatformPluginsSection() {
  return (
    <section className="page-section cluster-platform-plugins flex flex-col gap-2" aria-label="Platform plugins">
      <div className="px-3 pt-2">
        <h2 className="m-0 text-sm font-semibold">Platform plugins</h2>
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Shared infrastructure plugins — IB Gateway (redis-ib @ data NS) live status and Trade cutover probes.
        </p>
      </div>
      <IbGatewayLiveStatusPanel />
      <IbGatewayCutoverStatusPanel />
    </section>
  )
}
