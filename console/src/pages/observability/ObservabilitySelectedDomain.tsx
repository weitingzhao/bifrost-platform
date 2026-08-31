/**
 * Selected-domain evidence panel for ObservabilityPage (no-drift extract).
 */
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
  cn,
} from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import { StatusLamp } from '@/components/StatusLamp'
import type { ObservabilityViewModel } from '@/lib/observability'
import {
  shortMetricsPath,
  signalStateToVerdict,
  signalToGap,
} from '@/lib/observability'
import { GrafanaSoloEmbed } from '@/pages/observability/GrafanaSoloEmbed'
import {
  checkpointExpect,
  formatScrapeAge,
  GAP_LABEL,
  GAP_TAG_VARIANT,
  scrapeCell,
  SIGNAL_STATE_LABELS,
  verdictTag,
} from '@/pages/observability/observabilityFormat'

export function ObservabilitySelectedDomain({
  selected,
  selectedDomainHealthy,
  selectedRequiredSignals,
  checkpointsQuiet,
  dependencyQuiet,
  goldenQuiet,
  scrapeQuiet,
  selectedPrimaryGrafana,
  onNavigate,
}: {
  selected: ObservabilityViewModel['selected']
  selectedDomainHealthy: boolean
  selectedRequiredSignals: ObservabilityViewModel['domains'][number]['signals']
  checkpointsQuiet: boolean
  dependencyQuiet: boolean
  goldenQuiet: boolean
  scrapeQuiet: boolean
  selectedPrimaryGrafana: { label: string; url: string } | null
  onNavigate?: (tab: string) => void
}) {
  return (
    /* Selected Domain — nested flat OpsSections: CAUTION+ open, healthy collapsed */
    <OpsSection
      title={`Selected Domain · ${selected.domain}`}
      description="Expand a section to inspect · healthy sections stay collapsed"
      bodyPadding="compact"
      overflow="visible"
      actions={
        selectedPrimaryGrafana != null ? (
          <Button size="sm" variant="outline" asChild>
            <a href={selectedPrimaryGrafana.url} target="_blank" rel="noreferrer">
              Grafana · {selectedPrimaryGrafana.label}
            </a>
          </Button>
        ) : (
          <DenseTag variant="neutral" className="text-[9px]">
            Grafana unavailable
          </DenseTag>
        )
      }
    >
      <div className="flex flex-col gap-2">
        {selected.soloEmbed != null ? (
          <OpsSection
            key={`${selected.domain}-grafana`}
            title={selected.soloEmbed.title}
            description="Deep evidence · Grafana solo panel"
            variant="flat"
            className="rounded-md border border-[var(--border)] bg-[var(--secondary)]/25"
            collapsible
            defaultCollapsed={selectedDomainHealthy}
            bodyPadding="compact"
            overflow="hidden"
            actions={
              selectedPrimaryGrafana != null ? (
                <Button size="sm" variant="outline" asChild>
                  <a
                    href={selectedPrimaryGrafana.url}
                    target="_blank"
                    rel="noreferrer"
                    onClick={e => e.stopPropagation()}
                  >
                    Open in Grafana
                  </a>
                </Button>
              ) : null
            }
          >
            <GrafanaSoloEmbed
              url={selected.soloEmbed.url}
              title={selected.soloEmbed.title}
              height={selected.soloEmbed.height}
            />
          </OpsSection>
        ) : null}

        <OpsSection
          key={`${selected.domain}-checkpoints`}
          title="Checkpoints"
          description={
            selectedRequiredSignals.length === 0
              ? 'No required signals'
              : `${selectedRequiredSignals.length} required · ${
                  checkpointsQuiet ? 'all matched' : 'inspect gaps'
                }`
          }
          leading={
            <StatusLamp value={checkpointsQuiet ? 'ok' : 'degraded'} kind="reach" />
          }
          variant="flat"
          className="rounded-md border border-[var(--border)] bg-[var(--secondary)]/25"
          collapsible
          defaultCollapsed={checkpointsQuiet}
          bodyPadding="compact"
          overflow="hidden"
        >
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Checkpoint</DenseTableHead>
                <DenseTableHead>Scope</DenseTableHead>
                <DenseTableHead>Expect</DenseTableHead>
                <DenseTableHead>Actual</DenseTableHead>
                <DenseTableHead>Gap</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {selectedRequiredSignals.length === 0 ? (
                <DenseTableRow>
                  <DenseTableCell colSpan={5} className="text-muted-foreground">
                    No required signals for this domain.
                  </DenseTableCell>
                </DenseTableRow>
              ) : (
                selectedRequiredSignals.map(s => {
                  const gap = signalToGap(s)
                  return (
                    <DenseTableRow key={s.def.id}>
                      <DenseTableCell className="text-[var(--text-dense-meta)]" title={s.summary}>
                        {s.def.label}
                      </DenseTableCell>
                      <DenseTableCell>
                        <DenseTag variant="neutral" className="text-[9px] uppercase">
                          {s.def.scope}
                        </DenseTag>
                      </DenseTableCell>
                      <DenseTableCell className="text-[var(--text-dense-caption)] text-muted-foreground">
                        {checkpointExpect(s)}
                      </DenseTableCell>
                      <DenseTableCell>
                        <DenseTag variant={verdictTag(signalStateToVerdict(s.state))} className="text-[9px]">
                          {SIGNAL_STATE_LABELS[s.state]}
                        </DenseTag>
                      </DenseTableCell>
                      <DenseTableCell>
                        <DenseTag variant={GAP_TAG_VARIANT[gap]} className="text-[9px]">
                          {GAP_LABEL[gap]}
                        </DenseTag>
                      </DenseTableCell>
                    </DenseTableRow>
                  )
                })
              )}
            </DenseTableBody>
          </DenseDataTable>
        </OpsSection>

        <OpsSection
          key={`${selected.domain}-dependency`}
          title="Dependency path"
          description={
            selected.dependencyPath.length === 0
              ? 'No hops'
              : `${selected.dependencyPath.length} hops · ${
                  dependencyQuiet ? 'healthy' : 'issues in path'
                }`
          }
          leading={
            <StatusLamp value={dependencyQuiet ? 'ok' : 'degraded'} kind="reach" />
          }
          variant="flat"
          className="rounded-md border border-[var(--border)] bg-[var(--secondary)]/25"
          collapsible
          defaultCollapsed={dependencyQuiet}
          bodyPadding="compact"
          overflow="hidden"
        >
          {selected.dependencyPath.length === 0 ? (
            <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
              No required signals for this domain.
            </p>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {selected.dependencyPath.map(hop => (
                <div
                  key={hop.id}
                  className="rounded-md border border-[var(--border)] bg-[var(--secondary)] px-2 py-1"
                >
                  <span className="flex items-center gap-1.5">
                    <StatusLamp
                      value={
                        hop.state === 'healthy' || hop.state === 'expected_off'
                          ? 'ok'
                          : hop.state === 'degraded'
                            ? 'degraded'
                            : hop.state === 'critical'
                              ? 'fail'
                              : 'unknown'
                      }
                      kind="reach"
                    />
                    <span className="text-[var(--text-dense-caption)] font-medium">{hop.label}</span>
                    <DenseTag variant="neutral" className="text-[9px] uppercase">
                      {hop.scope}
                    </DenseTag>
                  </span>
                  <p className="m-0 mt-0.5 max-w-[16rem] truncate text-[var(--text-dense-caption)] text-muted-foreground">
                    {hop.summary}
                  </p>
                </div>
              ))}
            </div>
          )}
        </OpsSection>

        {(selected.domain === 'satellite' || selected.domain === 'ground-systems') && (
          <OpsSection
            key={`${selected.domain}-golden`}
            title="Golden signals"
            description={
              selected.goldenSignals.length === 0
                ? 'None for this domain'
                : `${selected.goldenSignals.length} signals · ${goldenQuiet ? 'ok' : 'review'}`
            }
            leading={<StatusLamp value={goldenQuiet ? 'ok' : 'degraded'} kind="reach" />}
            variant="flat"
            className="rounded-md border border-[var(--border)] bg-[var(--secondary)]/25"
            collapsible
            defaultCollapsed={goldenQuiet}
            bodyPadding="compact"
            overflow="hidden"
          >
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Signal</DenseTableHead>
                  <DenseTableHead>Value</DenseTableHead>
                  <DenseTableHead>Status</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {selected.goldenSignals.length === 0 ? (
                  <DenseTableRow>
                    <DenseTableCell colSpan={3} className="text-muted-foreground">
                      No golden signals for this domain
                    </DenseTableCell>
                  </DenseTableRow>
                ) : (
                  selected.goldenSignals.map(g => (
                    <DenseTableRow key={g.id}>
                      <DenseTableCell>{g.label}</DenseTableCell>
                      <DenseTableCell className="font-mono tabular-nums text-right">
                        {g.valueLabel}
                      </DenseTableCell>
                      <DenseTableCell className="text-muted-foreground">{g.status}</DenseTableCell>
                    </DenseTableRow>
                  ))
                )}
              </DenseTableBody>
            </DenseDataTable>
          </OpsSection>
        )}

        <OpsSection
          key={`${selected.domain}-scrape`}
          title="Scrape targets"
          description={selected.scrapeRollup.label}
          leading={<StatusLamp value={scrapeQuiet ? 'ok' : 'degraded'} kind="reach" />}
          variant="flat"
          className="rounded-md border border-[var(--border)] bg-[var(--secondary)]/25"
          collapsible
          defaultCollapsed={scrapeQuiet}
          bodyPadding="compact"
          overflow="hidden"
        >
          <DenseDataTable
            scrollX={false}
            wrapClassName="max-h-[14.5rem] overflow-y-auto"
            tableClassName="text-[var(--text-dense-caption)]"
          >
            <colgroup>
              <col className="w-[22%]" />
              <col className="w-[30%]" />
              <col className="w-[12%]" />
              <col className="w-[10%]" />
              <col className="w-[26%]" />
            </colgroup>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead className="!py-1 px-1.5 text-[var(--text-dense-micro)]">
                  Job / path
                </DenseTableHead>
                <DenseTableHead className="!py-1 px-1.5 text-[var(--text-dense-micro)]">
                  Node / instance
                </DenseTableHead>
                <DenseTableHead className="!py-1 px-1.5 text-[var(--text-dense-micro)]">
                  Health
                </DenseTableHead>
                <DenseTableHead className="!py-1 px-1.5 text-[var(--text-dense-micro)]">
                  Role
                </DenseTableHead>
                <DenseTableHead className="!py-1 px-1.5 text-[var(--text-dense-micro)]">
                  Last
                </DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {selected.scrapeTargets.length === 0 ? (
                <DenseTableRow>
                  <DenseTableCell colSpan={5} className={cn(scrapeCell, 'text-muted-foreground')}>
                    No targets mapped to this domain (or Prometheus unavailable)
                  </DenseTableCell>
                </DenseTableRow>
              ) : (
                selected.scrapeTargets.map(t => {
                  const expectedOff = t.expectedOff === true
                  const err = t.lastError != null && t.lastError !== '' ? t.lastError : null
                  const scrapeTitle = err ?? t.lastScrape ?? undefined
                  const pathLabel = shortMetricsPath(t.metricsPath)
                  const primaryHost = t.node ?? t.pod ?? t.instance
                  const hostTitle = [t.node, t.pod, t.instance].filter(Boolean).join(' · ')
                  const healthLamp =
                    t.health === 'up'
                      ? ('ok' as const)
                      : expectedOff
                        ? ('unknown' as const)
                        : t.health === 'down'
                          ? ('fail' as const)
                          : ('unknown' as const)
                  const healthLabel = expectedOff
                    ? 'EXPECTED OFF'
                    : t.health.toUpperCase()
                  return (
                    <DenseTableRow key={t.id}>
                      <DenseTableCell
                        className={cn(scrapeCell, 'font-mono-tabular')}
                        title={pathLabel != null ? `${t.job} ${t.metricsPath}` : t.job}
                      >
                        <span className="block truncate">{t.job}</span>
                        {pathLabel != null ? (
                          <span className="block truncate text-muted-foreground text-[var(--text-dense-micro)]">
                            {pathLabel}
                          </span>
                        ) : null}
                      </DenseTableCell>
                      <DenseTableCell
                        className={cn(scrapeCell, 'font-mono-tabular')}
                        title={hostTitle}
                      >
                        <span className="block truncate">{primaryHost}</span>
                        {t.node != null && t.node !== t.instance ? (
                          <span className="block truncate text-muted-foreground text-[var(--text-dense-micro)]">
                            {t.instance}
                          </span>
                        ) : null}
                      </DenseTableCell>
                      <DenseTableCell className={scrapeCell}>
                        <span className="inline-flex items-center gap-1 whitespace-nowrap">
                          <StatusLamp value={healthLamp} kind="reach" />
                          <span
                            className={cn(
                              'uppercase',
                              expectedOff
                                ? 'text-muted-foreground'
                                : t.health === 'down'
                                  ? 'text-danger'
                                  : 'text-muted-foreground',
                            )}
                          >
                            {healthLabel}
                          </span>
                        </span>
                      </DenseTableCell>
                      <DenseTableCell
                        className={cn(scrapeCell, 'uppercase text-muted-foreground truncate')}
                        title={t.role}
                      >
                        {t.role}
                      </DenseTableCell>
                      <DenseTableCell
                        className={cn(
                          scrapeCell,
                          'font-mono-tabular truncate',
                          err != null && !expectedOff
                            ? 'text-danger'
                            : 'text-muted-foreground',
                        )}
                        title={
                          expectedOff
                            ? err != null
                              ? `Standby expected off · ${err}`
                              : 'Standby expected off'
                            : scrapeTitle
                        }
                      >
                        {expectedOff
                          ? 'standby expected off'
                          : err != null
                            ? err
                            : formatScrapeAge(t.lastScrape)}
                      </DenseTableCell>
                    </DenseTableRow>
                  )
                })
              )}
            </DenseTableBody>
          </DenseDataTable>
        </OpsSection>

        <div className="flex flex-wrap gap-2 pt-1">
          {selected.detailLinks.map(link => (
            <Button
              key={link.route}
              variant="outline"
              size="sm"
              onClick={() => onNavigate?.(link.route)}
            >
              {link.label}
            </Button>
          ))}
          {selected.grafanaLinks.map(g =>
            g.available && g.url != null ? (
              <Button key={g.label} size="sm" asChild>
                <a href={g.url} target="_blank" rel="noreferrer">
                  Grafana · {g.label}
                </a>
              </Button>
            ) : (
              <DenseTag key={g.label} variant="neutral">
                Grafana · {g.label} · unavailable
              </DenseTag>
            ),
          )}
        </div>
      </div>
    </OpsSection>
  )
}
