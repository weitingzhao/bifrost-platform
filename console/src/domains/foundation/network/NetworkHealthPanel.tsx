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

interface NetworkHealthPanelProps {
  context: OpsContextResponse | undefined
  onOpenNetwork?: () => void
  /** Spine network-* infra streams. Status hides them; history is not a live signal. */
  showUpgradeRecord?: boolean
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
  onOpenNetwork,
  showUpgradeRecord = true,
  title = 'Network Health — ground floor (LAN / UniFi)',
  description = 'Spine network streams plus live UniFi probe via GET /api/v1/network/status + audit + health (Session v2 per D9).',
}: NetworkHealthPanelProps) {
  // Streams come only from the spine (GET /api/v1/context); nothing is filled in when it is not loaded.
  const spineLoaded = context?.tracks?.infra != null
  const streams = (context?.tracks?.infra?.streams ?? []).filter(s => s.id.startsWith('network-'))
  const liveProbe = useNetworkLiveProbe()

  const headerActions =
    onOpenNetwork != null ? (
      <Button variant="ghost" size="xs" onClick={onOpenNetwork}>
        Plugin → Network
      </Button>
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
        {showUpgradeRecord && <span>Streams {spineLoaded ? 'from spine' : 'unknown — spine not loaded'}</span>}
        <DenseTag variant={liveProbeTagVariant(liveProbe.probeReach)}>
          Live probe {liveProbe.isLoading ? '…' : liveProbe.probeReach.toUpperCase()}
        </DenseTag>
      </div>

      <div className="mb-4 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2">
        <div className="flex flex-wrap items-center gap-2">
          <StatusLamp value={liveProbe.probeReach} kind="reach" />
          <p className="m-0 text-[var(--text-dense-label)] font-medium">Live UniFi probe</p>
          <DenseTag variant="info">L0</DenseTag>
          <code className="text-[var(--text-dense-caption)] font-mono">GET /api/v1/network/status</code>
          <code className="text-[var(--text-dense-caption)] font-mono">GET /api/v1/network/audit</code>
          <code className="text-[var(--text-dense-caption)] font-mono">GET /api/v1/network/health</code>
        </div>
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {liveProbe.isLoading ? 'Probing UCG via platform-api…' : liveProbe.summary}
        </p>
        {liveProbe.audit?.classification === 'POLICY_DRIFT' && (
          <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-[var(--warning)]">
            Drift: {liveProbe.audit.zone_binding_gaps?.length ?? 0} zone gap(s) ·{' '}
            {liveProbe.audit.missing_policies?.length ?? 0} missing policy(ies)
          </p>
        )}
        {liveProbe.status?.hint != null && liveProbe.status.reachable !== true && (
          <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            {liveProbe.status.hint}
          </p>
        )}
        <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          Live probe polls platform-api every 30s — requires UNIFI_HOST/USER/PASS on platform-api host.
        </p>
      </div>

      {showUpgradeRecord && streams.length > 0 && (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Stream</DenseTableHead>
              <DenseTableHead className="text-right">Progress</DenseTableHead>
              <DenseTableHead>Note</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {streams.map(row => (
              <DenseTableRow key={row.id}>
                <DenseTableCell className="font-medium whitespace-nowrap">
                  <StatusLamp value={streamReach(row.done, row.total)} kind="reach" />
                  <span className="ml-2 font-mono text-xs">{row.id}</span>
                </DenseTableCell>
                <DenseTableCell className="text-right font-mono tabular-nums">
                  {row.done}/{row.total}
                </DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{row.note ?? row.label}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      )}

    </OpsSection>
  )
}
