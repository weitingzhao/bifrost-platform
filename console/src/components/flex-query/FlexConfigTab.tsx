import { useQuery } from '@tanstack/react-query'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
} from '@bifrost/ui'
import { fetchFlexConfigSummary, isProxyError, type FlexConfigSummary } from '@/api/flexQueryPlugin'
import { DashCard, Meter } from '@/components/market-data/overviewDash'
import { toneByLevel } from '@/components/market-data/overviewDashModel'
import { OpsSection } from '@/components/layout/OpsSection'

export function FlexConfigTab() {
  const q = useQuery({
    queryKey: ['flex-query', 'config', 'summary'],
    queryFn: fetchFlexConfigSummary,
    refetchInterval: 60_000,
    retry: 1,
  })
  const raw = q.data
  const err = raw != null && isProxyError(raw) ? raw.error : null
  let summary: FlexConfigSummary | null = null
  if (raw != null && !isProxyError(raw)) summary = raw

  return (
    <div className="flex flex-col gap-2">
      <OpsSection
        title="Tokens"
        description="Masked · edit in Trade Settings"
        bodyPadding="compact"
        overflow="visible"
        collapsible
        defaultCollapsed={false}
      >
        {q.isLoading ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Loading config…
          </p>
        ) : err != null ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{err}</p>
        ) : summary == null ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">—</p>
        ) : (
          <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
            {(
              [
                ['Host', summary.tokens.host_token_set, summary.tokens.host_token_last4],
                ['Secondary', summary.tokens.secondary_token_set, summary.tokens.secondary_token_last4],
              ] as const
            ).map(([label, set, last4]) => (
              <DashCard
                key={label}
                title={label}
                tag={set ? 'set' : 'not set'}
                tagVariant={set ? 'success' : 'neutral'}
                value={set ? `····${last4}` : '—'}
                caption="Flex Web Service token"
              >
                <Meter
                  fillPct={set ? 100 : 0}
                  toneClass={toneByLevel(set ? 'ok' : 'missing')}
                  label={label}
                />
              </DashCard>
            ))}
          </div>
        )}
      </OpsSection>

      <OpsSection
        title="Range days"
        description="Flex fetch window"
        bodyPadding="compact"
        overflow="visible"
        collapsible
        defaultCollapsed={false}
      >
        {summary == null && err == null && q.isLoading ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Loading range…
          </p>
        ) : summary == null ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">—</p>
        ) : (
          <p className="m-0 font-mono text-[var(--text-dense-meta)]">
            Default {summary.range_days.default}d · Init {summary.range_days.init}d
          </p>
        )}
      </OpsSection>

      <OpsSection
        title="Query rows"
        description="brokerage.settings_flex"
        bodyPadding="compact"
        overflow="visible"
        collapsible
        defaultCollapsed={false}
      >
        {q.isLoading ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Loading query rows…
          </p>
        ) : err != null ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{err}</p>
        ) : (summary?.query_rows ?? []).length === 0 ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            No brokerage.settings_flex rows
          </p>
        ) : (
          <OpsSection
            variant="flat"
            title="Query table"
            collapsible
            defaultCollapsed
            bodyPadding="none"
            overflow="visible"
          >
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Purpose</DenseTableHead>
                  <DenseTableHead>Label</DenseTableHead>
                  <DenseTableHead>Host query ID</DenseTableHead>
                  <DenseTableHead>Secondary query ID</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {(summary?.query_rows ?? []).map((row, i) => (
                  <DenseTableRow key={`${row.purpose}-${row.query_host_id}-${i}`}>
                    <DenseTableCell className="font-mono text-xs">{row.purpose}</DenseTableCell>
                    <DenseTableCell className="text-[var(--text-dense-meta)]">
                      {row.query_label ?? '—'}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono text-xs">
                      {row.query_host_id || '—'}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono text-xs">
                      {row.query_secondary_id ?? '—'}
                    </DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </OpsSection>
        )}
      </OpsSection>

      <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
        Read-only. Edit in Trade → Settings → IB Connection.
      </p>
    </div>
  )
}
