/**
 * Mission Control → Observability
 * One-screen answer: “Is the whole system healthy right now?”
 * Grafana is deep evidence — not a duplicated dashboard gallery.
 */

import { useMemo, useState } from 'react'
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
  PageHeader,
  SegmentControl,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  cn,
} from '@bifrost/ui'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { StatusLamp } from '@/components/StatusLamp'
import { useObservabilitySnapshot, type TradeEnv } from '@/hooks/useObservabilitySnapshot'
import {
  SYSTEM_DOMAIN_ICON,
  SYSTEM_DOMAIN_VARIANT,
  type SystemDomainId,
} from '@/lib/architecture/systemDomainCatalog'
import type {
  AttentionItem,
  DomainHealth,
  ObservabilityVerdict,
} from '@/lib/observability'
import { VERDICT_LABELS } from '@/lib/observability'

const TRADE_ENV_OPTIONS = [
  { value: 'dev', label: 'Dev' },
  { value: 'stg', label: 'Stg' },
  { value: 'prod', label: 'Prod' },
] as const

function verdictLamp(v: ObservabilityVerdict) {
  switch (v) {
    case 'healthy':
      return 'ok' as const
    case 'degraded':
      return 'degraded' as const
    case 'critical':
      return 'fail' as const
    default:
      return 'unknown' as const
  }
}

function verdictTag(v: ObservabilityVerdict): 'success' | 'warning' | 'danger' | 'neutral' {
  switch (v) {
    case 'healthy':
      return 'success'
    case 'degraded':
      return 'warning'
    case 'critical':
      return 'danger'
    default:
      return 'neutral'
  }
}

function severityLamp(s: AttentionItem['severity']) {
  if (s === 'critical') return 'fail' as const
  if (s === 'warning') return 'degraded' as const
  return 'unknown' as const
}

function formatFreshness(ms: number | null): string {
  if (ms == null) return 'unknown'
  if (ms < 60_000) return `${Math.round(ms / 1000)}s ago`
  return `${Math.round(ms / 60_000)}m ago`
}

function DomainCard({
  domain,
  selected,
  onSelect,
}: {
  domain: DomainHealth
  selected: boolean
  onSelect: () => void
}) {
  const Icon = SYSTEM_DOMAIN_ICON[domain.domain]
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        'flex min-w-[9.5rem] flex-1 flex-col gap-1 rounded-md border px-2.5 py-2 text-left transition-colors',
        selected
          ? 'border-[var(--ring)] bg-[var(--accent)]'
          : 'border-[var(--border)] bg-[var(--secondary)] hover:bg-[var(--accent)]/60',
      )}
    >
      <span className="flex items-center gap-1.5">
        <Icon className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
        <span className="text-[var(--text-dense-caption)] font-medium">{domain.label}</span>
      </span>
      <span className="flex items-center gap-1.5">
        <StatusLamp value={verdictLamp(domain.verdict)} kind="reach" />
        <DenseTag variant={verdictTag(domain.verdict)} className="text-[9px]">
          {VERDICT_LABELS[domain.verdict]}
        </DenseTag>
        {domain.alertCount > 0 && (
          <DenseTag variant="warning" className="text-[9px]">
            {domain.alertCount} alert{domain.alertCount === 1 ? '' : 's'}
          </DenseTag>
        )}
      </span>
      <span className="line-clamp-2 text-[var(--text-dense-caption)] text-muted-foreground" title={domain.reason}>
        {domain.reason}
      </span>
      <span className="text-[var(--text-dense-caption)] text-muted-foreground font-mono-tabular">
        coverage {domain.coverage.observed}/{domain.coverage.required}
        {domain.envScope !== 'none' ? ` · ${domain.envScope}` : ''}
      </span>
    </button>
  )
}

export function ObservabilityPage({
  onNavigate,
}: {
  onNavigate?: (tab: string) => void
}) {
  const {
    viewModel,
    tradeEnv,
    setTradeEnv,
    selectedDomain,
    setSelectedDomain,
    isLoading,
    isFetching,
    refetchAll,
    namespace,
  } = useObservabilitySnapshot()

  const [attentionDetail, setAttentionDetail] = useState<AttentionItem | null>(null)
  const system = viewModel.system
  const selected = viewModel.selected

  const domainCountsLabel = useMemo(() => {
    const c = system.domainCounts
    return [
      c.critical > 0 ? `${c.critical} critical` : null,
      c.degraded > 0 ? `${c.degraded} degraded` : null,
      c.unknown > 0 ? `${c.unknown} unknown` : null,
      c.not_observed > 0 ? `${c.not_observed} not observed` : null,
      c.healthy > 0 ? `${c.healthy} healthy` : null,
    ]
      .filter(Boolean)
      .join(' · ')
  }, [system.domainCounts])

  return (
    <div className="flex flex-col gap-3">
      <PageHeader
        title="Observability"
        description="Apollo-domain system health hub — Grafana is deep evidence, not a second control plane."
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs font-medium text-muted-foreground shrink-0">Trade NS:</span>
            <SegmentControl
              value={tradeEnv}
              onChange={v => setTradeEnv(v as TradeEnv)}
              options={[...TRADE_ENV_OPTIONS]}
            />
            <SectionRefreshButton isFetching={isFetching} onClick={refetchAll} />
          </div>
        }
      />

      {/* System Verdict */}
      <section className="page-section panel-elevated px-3 py-2.5">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <StatusLamp value={verdictLamp(system.overall)} kind="reach" />
          <span className="text-[var(--text-dense-label)] font-semibold tracking-wide">
            SYSTEM VERDICT · {tradeEnv.toUpperCase()}
          </span>
          <DenseTag variant={verdictTag(system.overall)} className="text-[10px] font-semibold">
            {isLoading ? 'PROBING' : system.label}
          </DenseTag>
          {system.stale && (
            <DenseTag variant="warning" className="text-[9px]">
              STALE
            </DenseTag>
          )}
          <span
            className="min-w-0 flex-1 truncate text-[var(--text-dense-meta)]"
            title={system.primaryCause}
          >
            {isLoading ? 'Aggregating probes…' : system.primaryCause}
          </span>
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[var(--text-dense-caption)] text-muted-foreground">
          <span>{domainCountsLabel || '—'}</span>
          <span className="font-mono-tabular">
            alerts {system.firingAlerts} firing · {system.mappedFiringAlerts} mapped
          </span>
          <span className="font-mono-tabular">freshness {formatFreshness(system.freshnessMs)}</span>
          <span className="font-mono-tabular">{namespace}</span>
          <span className="ml-auto">
            Layer B {viewModel.layerBStatus}
            {!viewModel.prometheusConfigured ? ' · Prometheus not configured' : ''}
          </span>
        </div>
        {!viewModel.prometheusConfigured && !isLoading && (
          <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-muted-foreground">
            Missing scrape data is shown as UNKNOWN / NOT OBSERVED — never as HEALTHY. Install Layer B via
            Rocket → Cluster, then return here for system verdict.
          </p>
        )}
      </section>

      {/* Apollo Domain Health */}
      <OpsSection
        title="Apollo Domain Health"
        description="Seven fixed domains — click to inspect selected domain detail"
        bodyPadding="compact"
        overflow="visible"
      >
        <div className="flex flex-wrap gap-1.5">
          {viewModel.domains.map(d => (
            <DomainCard
              key={d.domain}
              domain={d}
              selected={selectedDomain === d.domain}
              onSelect={() => setSelectedDomain(d.domain)}
            />
          ))}
        </div>
      </OpsSection>

      {/* Attention */}
      <OpsSection
        title="Attention"
        description="Severity · Domain · Environment · Signal · Since · Owner · Action"
        bodyPadding="none"
        overflow="hidden"
      >
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Severity</DenseTableHead>
              <DenseTableHead>Domain</DenseTableHead>
              <DenseTableHead>Environment</DenseTableHead>
              <DenseTableHead>Signal</DenseTableHead>
              <DenseTableHead>Since</DenseTableHead>
              <DenseTableHead>Owner</DenseTableHead>
              <DenseTableHead>Action</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {isLoading ? (
              <DenseTableRow>
                <DenseTableCell colSpan={7} className="text-muted-foreground">
                  Loading…
                </DenseTableCell>
              </DenseTableRow>
            ) : viewModel.attention.length === 0 ? (
              <DenseTableRow>
                <DenseTableCell colSpan={7} className="text-muted-foreground">
                  <span className="inline-flex items-center gap-1.5">
                    <StatusLamp value="ok" kind="reach" />
                    No attention items — required signals clear for observed domains.
                  </span>
                </DenseTableCell>
              </DenseTableRow>
            ) : (
              viewModel.attention.map(item => (
                <DenseTableRow key={item.id}>
                  <DenseTableCell>
                    <span className="inline-flex items-center gap-1">
                      <StatusLamp value={severityLamp(item.severity)} kind="reach" />
                      <span className="text-[var(--text-dense-caption)] uppercase">{item.severity}</span>
                    </span>
                  </DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={SYSTEM_DOMAIN_VARIANT[item.domain]} className="text-[9px]">
                      {item.domain}
                    </DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="font-mono-tabular text-[var(--text-dense-caption)]">
                    {item.env}
                  </DenseTableCell>
                  <DenseTableCell className="text-[var(--text-dense-meta)]" title={item.summary}>
                    {item.signalLabel}
                  </DenseTableCell>
                  <DenseTableCell className="font-mono-tabular text-[var(--text-dense-caption)]">
                    {item.since != null ? new Date(item.since).toLocaleString() : '—'}
                  </DenseTableCell>
                  <DenseTableCell className="text-[var(--text-dense-caption)]">{item.owner}</DenseTableCell>
                  <DenseTableCell>
                    <button
                      type="button"
                      className="focus-strip-link text-[var(--text-dense-caption)]"
                      onClick={() => setAttentionDetail(item)}
                    >
                      Inspect
                    </button>
                  </DenseTableCell>
                </DenseTableRow>
              ))
            )}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

      {/* Selected Domain */}
      <OpsSection
        title={`Selected Domain · ${selected.domain}`}
        description="Dependency path · golden signals · alerts · scrape coverage · detail / Grafana links"
        bodyPadding="compact"
        overflow="visible"
      >
        <div className="flex flex-col gap-3">
          <div>
            <OpsSubsectionTitle className="mb-1">Dependency path</OpsSubsectionTitle>
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
          </div>

          {(selected.domain === 'satellite' || selected.domain === 'ground-systems') && (
            <div>
              <OpsSubsectionTitle className="mb-1">Golden signals</OpsSubsectionTitle>
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
            </div>
          )}

          <div>
            <OpsSubsectionTitle className="mb-1">Scrape targets</OpsSubsectionTitle>
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Job</DenseTableHead>
                  <DenseTableHead>Instance</DenseTableHead>
                  <DenseTableHead>Health</DenseTableHead>
                  <DenseTableHead>Role</DenseTableHead>
                  <DenseTableHead>Last scrape / error</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {selected.scrapeTargets.length === 0 ? (
                  <DenseTableRow>
                    <DenseTableCell colSpan={5} className="text-muted-foreground">
                      No targets mapped to this domain (or Prometheus unavailable)
                    </DenseTableCell>
                  </DenseTableRow>
                ) : (
                  selected.scrapeTargets.slice(0, 24).map(t => (
                    <DenseTableRow key={t.id}>
                      <DenseTableCell className="font-mono-tabular text-[var(--text-dense-caption)]">
                        {t.job}
                      </DenseTableCell>
                      <DenseTableCell className="font-mono-tabular text-[var(--text-dense-caption)]">
                        {t.instance}
                      </DenseTableCell>
                      <DenseTableCell>
                        <StatusLamp
                          value={t.health === 'up' ? 'ok' : t.health === 'down' ? 'fail' : 'unknown'}
                          kind="reach"
                        />{' '}
                        <span className="text-[var(--text-dense-caption)] uppercase">{t.health}</span>
                      </DenseTableCell>
                      <DenseTableCell>
                        <DenseTag variant="neutral" className="text-[9px] uppercase">
                          {t.role}
                        </DenseTag>
                      </DenseTableCell>
                      <DenseTableCell className="text-[var(--text-dense-caption)] text-muted-foreground">
                        {t.lastError != null && t.lastError !== ''
                          ? t.lastError
                          : (t.lastScrape ?? '—')}
                      </DenseTableCell>
                    </DenseTableRow>
                  ))
                )}
              </DenseTableBody>
            </DenseDataTable>
          </div>

          <div className="flex flex-wrap gap-2">
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

      {/* Grafana catalog */}
      <OpsSection
        title="Grafana dashboards"
        description="Domain · Dashboard · Environment · Purpose · Availability — complex charts stay in Grafana"
        bodyPadding="none"
        overflow="hidden"
      >
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Domain</DenseTableHead>
              <DenseTableHead>Dashboard</DenseTableHead>
              <DenseTableHead>Environment</DenseTableHead>
              <DenseTableHead>Purpose</DenseTableHead>
              <DenseTableHead>Availability</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {viewModel.dashboards.map(d => (
              <DenseTableRow key={d.id}>
                <DenseTableCell>
                  <DenseTag variant={SYSTEM_DOMAIN_VARIANT[d.domain as SystemDomainId]} className="text-[9px]">
                    {d.domain}
                  </DenseTag>
                </DenseTableCell>
                <DenseTableCell className="font-medium text-[var(--text-dense-meta)]">{d.title}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-caption)]">
                  {d.env}
                </DenseTableCell>
                <DenseTableCell className="text-[var(--text-dense-caption)] text-muted-foreground">
                  {d.purpose}
                </DenseTableCell>
                <DenseTableCell>
                  {d.available && d.url != null ? (
                    <a
                      href={d.url}
                      target="_blank"
                      rel="noreferrer"
                      className="text-[var(--text-dense-caption)] text-primary underline-offset-2 hover:underline"
                    >
                      Open
                    </a>
                  ) : (
                    <DenseTag variant="neutral" className="text-[9px]">
                      unavailable
                    </DenseTag>
                  )}
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

      <Sheet open={attentionDetail != null} onOpenChange={open => !open && setAttentionDetail(null)}>
        <SheetContent side="right" className="w-full overflow-y-auto sm:max-w-md">
          {attentionDetail != null && (
            <>
              <SheetHeader>
                <SheetTitle className="flex items-center gap-2">
                  <StatusLamp value={severityLamp(attentionDetail.severity)} kind="reach" />
                  {attentionDetail.signalLabel}
                </SheetTitle>
                <SheetDescription>
                  {attentionDetail.domain} · {attentionDetail.env} · {attentionDetail.owner}
                </SheetDescription>
              </SheetHeader>
              <div className="flex flex-col gap-3 px-4 pb-4">
                {(
                  [
                    ['What happened', attentionDetail.triage.whatHappened],
                    ['Why verdict changed', attentionDetail.triage.whyVerdictChanged],
                    ['Affected domains', attentionDetail.triage.affectedDomains.join(', ')],
                    ['Evidence', attentionDetail.triage.evidence],
                    ['Recommended destination', attentionDetail.triage.recommendedDestination],
                  ] as const
                ).map(([label, value]) => (
                  <div key={label}>
                    <p className="m-0 mb-0.5 text-[var(--text-dense-caption)] font-medium text-muted-foreground">
                      {label}
                    </p>
                    <p className="m-0 text-[var(--text-dense-meta)]">{value}</p>
                  </div>
                ))}
                <div className="flex flex-wrap gap-2 border-t border-[var(--border)] pt-2">
                  {attentionDetail.triage.detailRoute != null && (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        onNavigate?.(attentionDetail.triage.detailRoute!)
                        setAttentionDetail(null)
                      }}
                    >
                      Open detail
                    </Button>
                  )}
                  {attentionDetail.triage.grafanaUrl != null && (
                    <Button size="sm" asChild>
                      <a href={attentionDetail.triage.grafanaUrl} target="_blank" rel="noreferrer">
                        Open Grafana
                      </a>
                    </Button>
                  )}
                </div>
              </div>
            </>
          )}
        </SheetContent>
      </Sheet>
    </div>
  )
}
