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
import type { OpsContextResponse } from '@/api/opsContextTypes'
import { OpsSection } from '@/components/layout/OpsSection'
import { useNetworkLiveProbe } from '@/hooks/useNetworkLiveProbe'
import {
  NETWORK_HEALTH_PROJECTION,
  resolveNetworkStreamProjections,
} from '@/lib/architecture/networkConsoleProjection'

interface NetworkHealthPanelProps {
  context: OpsContextResponse | undefined
  onOpenAgentProtocol: () => void
  onOpenNetwork?: () => void
  /** When false, hide header Agent Protocol (page Verdict already owns it). Default true for Control Room. */
  showPrimaryAgentAction?: boolean
  /** Section title — Control Room keeps Health; Ground Network page uses evidence wording. */
  title?: string
  description?: string
}

function streamReach(done: number, total: number): 'ok' | 'degraded' | 'unknown' {
  if (total <= 0) return 'unknown'
  if (done >= total) return 'ok'
  if (done > 0) return 'degraded'
  return 'unknown'
}

function liveProbeTagVariant(
  reach: ReturnType<typeof useNetworkLiveProbe>['probeReach'],
): 'success' | 'warning' | 'danger' | 'neutral' {
  if (reach === 'ok') return 'success'
  if (reach === 'degraded') return 'warning'
  if (reach === 'fail') return 'danger'
  return 'neutral'
}

export function NetworkHealthPanel({
  context,
  onOpenAgentProtocol,
  onOpenNetwork,
  showPrimaryAgentAction = true,
  title = 'Network Health — ground floor (LAN / UniFi)',
  description = 'Catalog + spine projection plus live UniFi probe via GET /api/v1/network/status + audit (Session v2 per D9).',
}: NetworkHealthPanelProps) {
  const streams = resolveNetworkStreamProjections(context)
  const spineLoaded = context?.tracks?.infra != null
  const { firewall } = NETWORK_HEALTH_PROJECTION
  const liveProbe = useNetworkLiveProbe()

  const headerActions =
    onOpenNetwork != null || showPrimaryAgentAction ? (
      <div className="flex flex-wrap gap-2">
        {onOpenNetwork != null && (
          <Button variant="ghost" size="xs" onClick={onOpenNetwork}>
            Ground Systems → Network
          </Button>
        )}
        {showPrimaryAgentAction && (
          <Button variant="ghost" size="xs" onClick={onOpenAgentProtocol}>
            Agent Protocol
          </Button>
        )}
      </div>
    ) : undefined

  return (
    <OpsSection
      title={title}
      description={description}
      actions={headerActions}
      bodyPadding="default"
      overflow="visible"
    >
      <div className="mb-3 flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        <DenseTag variant="info">{NETWORK_HEALTH_PROJECTION.status}</DenseTag>
        <span>
          Catalog <strong>{NETWORK_HEALTH_PROJECTION.catalogVersion}</strong>
        </span>
        <span>· Projection {spineLoaded ? 'spine + catalog' : 'catalog fallback'}</span>
        <DenseTag variant={liveProbeTagVariant(liveProbe.probeReach)}>
          Live probe {liveProbe.isLoading ? '…' : liveProbe.probeReach.toUpperCase()}
        </DenseTag>
      </div>

      <div className="mb-4 rounded-md border border-[var(--border)] bg-[var(--secondary)] px-3 py-2">
        <div className="flex flex-wrap items-center gap-2">
          <StatusLamp value={liveProbe.probeReach} kind="reach" />
          <p className="m-0 text-[var(--text-dense-label)] font-medium">Live UniFi probe</p>
          <DenseTag variant="info">L0</DenseTag>
          <code className="text-[var(--text-dense-caption)] font-mono">GET /api/v1/network/status</code>
          <code className="text-[var(--text-dense-caption)] font-mono">GET /api/v1/network/audit</code>
        </div>
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {liveProbe.isLoading ? 'Probing UCG via platform-api…' : liveProbe.summary}
        </p>
        {liveProbe.audit?.classification === 'POLICY_DRIFT' && (
          <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-[var(--warning)]">
            Drift: {liveProbe.audit.zone_binding_gaps?.length ?? 0} zone gap(s) ·{' '}
            {liveProbe.audit.missing_policies?.length ?? 0} missing policy(ies) — Agent{' '}
            <button type="button" className="focus-strip-link" onClick={onOpenAgentProtocol}>
              POLICY_DRIFT remediation
            </button>
          </p>
        )}
        {liveProbe.status?.hint != null && liveProbe.status.reachable !== true && (
          <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            {liveProbe.status.hint}
          </p>
        )}
        <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          {NETWORK_HEALTH_PROJECTION.liveProbeNote}
        </p>
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
      </div>
    </OpsSection>
  )
}
