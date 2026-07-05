import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  StatusLamp,
} from '@bifrost/ui'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'
import { OpsSection } from '@/components/layout/OpsSection'

function reachTagVariant(reach: string): 'success' | 'warning' | 'danger' | 'neutral' {
  if (reach === 'ok') return 'success'
  if (reach === 'degraded') return 'warning'
  if (reach === 'fail') return 'danger'
  return 'neutral'
}

export function IbGatewayCutoverStatusPanel() {
  const liveProbe = useIbGatewayLiveProbe()
  const cutover = liveProbe.status?.cutover

  return (
    <OpsSection
      title="Trade cutover status"
      description="L0 probe — legacy IB StatefulSets retired + redis-ib ExternalName per Trade namespace (IBGP3)."
      bodyPadding="default"
    >
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <StatusLamp value={cutover?.reachability ?? 'unknown'} kind="reach" />
        <DenseTag variant={reachTagVariant(cutover?.reachability ?? 'unknown')}>
          {cutover?.legacy_socket_retired === true ? 'LEGACY RETIRED' : 'LEGACY ACTIVE'}
        </DenseTag>
        {liveProbe.isLoading && (
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Probing…</span>
        )}
      </div>

      {cutover?.environments != null && cutover.environments.length > 0 ? (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Namespace</DenseTableHead>
              <DenseTableHead>Legacy IB replicas</DenseTableHead>
              <DenseTableHead>redis-ib ExternalName</DenseTableHead>
              <DenseTableHead>Reach</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {cutover.environments.map(env => (
              <DenseTableRow key={env.namespace}>
                <DenseTableCell>{env.namespace}</DenseTableCell>
                <DenseTableCell>{env.legacy_ib_replicas}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={env.redis_ib_external_name_ok ? 'success' : 'danger'}>
                    {env.redis_ib_external_name_ok ? 'OK' : 'MISSING'}
                  </DenseTag>
                </DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={reachTagVariant(env.reachability)}>{env.reachability}</DenseTag>
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      ) : (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Cutover probe unavailable — ensure platform-api includes cutover in ib-gateway status.
        </p>
      )}
    </OpsSection>
  )
}
