import {
  Button,
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
import type { OpsContextResponse } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  NETWORK_HEALTH_PROJECTION,
  resolveNetworkStreamProjections,
} from '@/lib/architecture/networkConsoleProjection'

interface NetworkHealthPanelProps {
  context: OpsContextResponse | undefined
  onOpenNetworkUpgrade: () => void
  onOpenNetworkApi: () => void
  onOpenAgentProtocol: () => void
}

function streamReach(done: number, total: number): 'ok' | 'degraded' | 'unknown' {
  if (total <= 0) return 'unknown'
  if (done >= total) return 'ok'
  if (done > 0) return 'degraded'
  return 'unknown'
}

export function NetworkHealthPanel({
  context,
  onOpenNetworkUpgrade,
  onOpenNetworkApi,
  onOpenAgentProtocol,
}: NetworkHealthPanelProps) {
  const streams = resolveNetworkStreamProjections(context)
  const spineLoaded = context?.tracks?.infra != null
  const { firewall } = NETWORK_HEALTH_PROJECTION

  return (
    <OpsSection
      title="Network Health — ground floor (LAN / UniFi)"
      description="Catalog + spine projection — firewall applied via Session v2 (D9). Live zone-matrix probe via platform-api planned (North Star /api/v1/network/*)."
      actions={
        <div className="flex flex-wrap gap-2">
          <Button variant="ghost" size="xs" onClick={onOpenNetworkUpgrade}>
            Network Upgrade
          </Button>
          <Button variant="ghost" size="xs" onClick={onOpenNetworkApi}>
            Network API contract
          </Button>
          <Button variant="ghost" size="xs" onClick={onOpenAgentProtocol}>
            Agent Protocol
          </Button>
        </div>
      }
      bodyPadding="default"
      overflow="visible"
    >
      <div className="mb-3 flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        <DenseTag variant="info">{NETWORK_HEALTH_PROJECTION.status}</DenseTag>
        <span>
          Catalog <strong>{NETWORK_HEALTH_PROJECTION.catalogVersion}</strong>
        </span>
        <span>· Projection {spineLoaded ? 'spine + catalog' : 'catalog fallback'}</span>
      </div>

      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Stream</DenseTableHead>
            <DenseTableHead className="text-right">Progress</DenseTableHead>
            <DenseTableHead>Source</DenseTableHead>
            <DenseTableHead>Note</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {streams.map(row => (
            <DenseTableRow key={row.stream}>
              <DenseTableCell className="font-medium whitespace-nowrap">
                <StatusLamp value={streamReach(row.done, row.total)} kind="reach" />
                <span className="ml-2 font-mono text-xs">{row.stream}</span>
              </DenseTableCell>
              <DenseTableCell className="text-right font-mono tabular-nums">
                {row.done}/{row.total}
              </DenseTableCell>
              <DenseTableCell>
                <DenseTag variant={row.source === 'spine' ? 'success' : 'neutral'}>{row.source}</DenseTag>
              </DenseTableCell>
              <DenseTableCell className="text-[var(--muted-foreground)]">{row.note}</DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>

      <div className="mt-4 rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2">
        <p className="m-0 text-[var(--text-dense-label)] font-medium">
          ZBF firewall applied · spine {firewall.spineDecision}
        </p>
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {firewall.appliedAt} · {firewall.zoneCount} Bifrost zones · {firewall.policyCount} policies ·{' '}
          {firewall.actuationPath}
        </p>
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Drift audit: <code className="font-mono text-xs">{firewall.auditScript}</code> vs{' '}
          <code className="font-mono text-xs">FIREWALL_RULES</code> — Agent{' '}
          <button type="button" className="focus-strip-link" onClick={onOpenAgentProtocol}>
            POLICY_NOMINAL / POLICY_DRIFT
          </button>
        </p>
        <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          {NETWORK_HEALTH_PROJECTION.futureProbe}
        </p>
      </div>
    </OpsSection>
  )
}
