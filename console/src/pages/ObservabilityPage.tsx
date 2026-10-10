/**
 * Mission Control → Observability
 * One-screen answer: “Is the whole system healthy right now?”
 * Grafana is deep evidence — not a duplicated dashboard gallery.
 *
 * Attention: triage entry only — Inspect / Mute / Manual next. Agents are not
 * started from this page.
 */

import { useMemo, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Button, DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag } from '@bifrost/ui'
import { postAttentionMute } from '@/api/telemetry'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { StatusLamp } from '@/components/StatusLamp'
import { useObservabilitySnapshot } from '@/hooks/useObservabilitySnapshot'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  SYSTEM_DOMAIN_VARIANT,
  type SystemDomainId,
} from '@/lib/architecture/systemDomainCatalog'
import type { AttentionItem } from '@/lib/observability'
import {
  ATTENTION_MUTE_DEFAULT_HOURS,
  filterMutedAttention,
  listActiveAttentionMutes,
  muteAttentionIds,
  signalToGap,
  sumGapSummaries,
  VERDICT_LABELS,
} from '@/lib/observability'
import { DomainCard } from '@/pages/observability/DomainCard'
import { ObservabilityAttentionPanel } from '@/pages/observability/ObservabilityAttentionPanel'
import { ObservabilitySelectedDomain } from '@/pages/observability/ObservabilitySelectedDomain'
import { ReferenceDomainChip } from '@/pages/observability/ReferenceDomainChip'
import { SystemGapMeta } from '@/pages/observability/SystemGapMeta'
import {
  attentionMatchesScope,
  formatFreshness,
  GAP_LEGEND,
  primaryGrafanaForDomain,
  rollupDomainVerdict,
  verdictLamp,
  verdictTag,
  type AttentionScopeFilter,
} from '@/pages/observability/observabilityFormat'

export function ObservabilityPage({
  onNavigate,
  lockToViewer = false,
}: {
  onNavigate?: (tab: string) => void
  /** Status page: scope probes to self-health viewer_env. No environment selector. */
  lockToViewer?: boolean
}) {
  const [evidenceOn, setEvidenceOn] = useState(!lockToViewer)
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
  } = useObservabilitySnapshot({ followViewerOnly: lockToViewer, includeEvidence: evidenceOn })
  const { canOperate } = usePlatformAuth()

  const [attentionDetail, setAttentionDetail] = useState<AttentionItem | null>(null)
  const [attentionScope, setAttentionScope] = useState<AttentionScopeFilter>('all')
  const [muteRevision, setMuteRevision] = useState(0)
  const [muteConfirmItem, setMuteConfirmItem] = useState<AttentionItem | null>(null)
  const [muteMessage, setMuteMessage] = useState<string | null>(null)
  const system = viewModel.system
  const selected = viewModel.selected

  const muteMutation = useMutation({
    mutationFn: async (item: AttentionItem) => {
      muteAttentionIds(
        [{ attentionId: item.id, signalLabel: item.signalLabel }],
        ATTENTION_MUTE_DEFAULT_HOURS,
      )
      setMuteRevision(n => n + 1)
      if (!canOperate) {
        return {
          ok: true,
          message: 'Muted in this browser only (no operator token — not audited / no Alertmanager)',
        }
      }
      return postAttentionMute({
        attention_id: item.id,
        signal_label: item.signalLabel,
        domain: item.domain,
        env: item.env,
        alertname: item.signalLabel,
        duration_hours: ATTENTION_MUTE_DEFAULT_HOURS,
        comment: `Observability Attention mute ${ATTENTION_MUTE_DEFAULT_HOURS}h · ${item.id}`,
      })
    },
    onSuccess: data => {
      setMuteMessage(data.message)
      setMuteConfirmItem(null)
      setAttentionDetail(null)
    },
    onError: (err: Error) => {
      // Local mute already applied — surface server/AM failure.
      setMuteMessage(`Muted in UI; server: ${err.message}`)
      setMuteConfirmItem(null)
    },
  })

  const runtimeDomains = useMemo(
    () => viewModel.domains.filter(d => d.probeability === 'runtime'),
    [viewModel.domains],
  )
  /** Satellite (and any pure env-scoped domain) — follows Trade env selector. */
  const tradeEnvDomains = useMemo(
    () => runtimeDomains.filter(d => d.envScope === 'env'),
    [runtimeDomains],
  )
  /** Rocket / Ground / Subcontractors / Engineer — cluster fabric, not Trade-NS-scoped. */
  const sharedPlatformDomains = useMemo(
    () => runtimeDomains.filter(d => d.envScope !== 'env'),
    [runtimeDomains],
  )
  /** Unified Domain Health row — Trade-scoped first, then shared (no separate section). */
  const runtimeDomainCards = useMemo(
    () => [...tradeEnvDomains, ...sharedPlatformDomains],
    [tradeEnvDomains, sharedPlatformDomains],
  )
  const referenceDomains = useMemo(
    () => viewModel.domains.filter(d => d.probeability === 'reference'),
    [viewModel.domains],
  )
  const tradeEnvRollup = useMemo(() => rollupDomainVerdict(tradeEnvDomains), [tradeEnvDomains])
  const sharedRollup = useMemo(
    () => rollupDomainVerdict(sharedPlatformDomains),
    [sharedPlatformDomains],
  )
  const filteredAttention = useMemo(() => {
    void muteRevision
    const scoped = viewModel.attention.filter(item => attentionMatchesScope(item, attentionScope))
    return filterMutedAttention(scoped)
  }, [viewModel.attention, attentionScope, muteRevision])

  const activeMuteCount = useMemo(() => {
    void muteRevision
    return listActiveAttentionMutes().length
  }, [muteRevision])

  const domainCountsLabel = useMemo(() => {
    const c = system.domainCounts
    const parts = [
      c.critical > 0 ? `${c.critical} critical` : null,
      c.degraded > 0 ? `${c.degraded} degraded` : null,
      c.unknown > 0 ? `${c.unknown} unknown` : null,
      c.not_observed > 0 ? `${c.not_observed} not observed` : null,
      c.healthy > 0 ? `${c.healthy} healthy` : null,
    ]
      .filter(Boolean)
      .join(' · ')
    return parts
  }, [system.domainCounts])

  /** Runtime domains only — reference planes excluded from primary gap meta. */
  const systemGapSummary = useMemo(
    () => sumGapSummaries(viewModel.domains),
    [viewModel.domains],
  )

  const selectedRequiredSignals = useMemo(() => {
    const domain = viewModel.domains.find(d => d.domain === selectedDomain)
    return (domain?.signals ?? []).filter(s => s.def.role === 'required')
  }, [viewModel.domains, selectedDomain])

  const selectedDomainHealthy = useMemo(() => {
    const d = viewModel.domains.find(x => x.domain === selectedDomain)
    return d?.verdict === 'healthy'
  }, [viewModel.domains, selectedDomain])

  const checkpointsQuiet =
    selectedRequiredSignals.length > 0 &&
    selectedRequiredSignals.every(s => {
      const gap = signalToGap(s)
      return gap === 'ok' || gap === 'by_design'
    })

  const dependencyQuiet =
    selected.dependencyPath.length > 0 &&
    selected.dependencyPath.every(
      hop => hop.state === 'healthy' || hop.state === 'expected_off',
    )

  const goldenQuiet =
    selected.goldenSignals.length > 0 &&
    selected.goldenSignals.every(g => g.status === 'ok')

  const scrapeQuiet = selected.scrapeRollup.quiet

  const systemHealthy = !isLoading && system.overall === 'healthy'
  const selectedPrimaryGrafana = useMemo(() => {
    const hit = selected.grafanaLinks.find(g => g.available && g.url != null)
    return hit?.url != null ? { label: hit.label, url: hit.url } : null
  }, [selected.grafanaLinks])
  const attentionQuiet =
    !isLoading && viewModel.attention.length === 0 && system.firingAlerts === 0

  const scrollToAttention = () => {
    document.getElementById('obs-attention')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  return (
    <div className="flex flex-col gap-3">
      <OpsVerdictStrip
        ariaLabel="System verdict"
        title={`SYSTEM VERDICT · ${tradeEnv.toUpperCase()}`}
        lamp={verdictLamp(system.overall)}
        tagLabel={isLoading ? 'PROBING' : system.label}
        tagVariant={verdictTag(system.overall)}
        summary={
          <span className="inline-flex min-w-0 max-w-full items-center gap-2">
            {system.stale && (
              <DenseTag variant="warning" className="shrink-0 text-[9px]">
                STALE
              </DenseTag>
            )}
            <span className="truncate" title={system.primaryCause}>
              {isLoading ? 'Aggregating probes…' : system.primaryCause}
            </span>
          </span>
        }
        actions={
          lockToViewer && !evidenceOn ? (
            <Button variant="outline" size="sm" onClick={() => setEvidenceOn(true)}>
              Load evidence
            </Button>
          ) : undefined
        }
        meta={
          <>
            <span
              className="inline-flex min-w-0 max-w-full items-center gap-1"
              title={tradeEnvRollup.cause}
            >
              <span className="text-muted-foreground shrink-0">Trade env</span>
              <StatusLamp value={verdictLamp(tradeEnvRollup.verdict)} kind="reach" />
              <DenseTag variant={verdictTag(tradeEnvRollup.verdict)} className="text-[9px] shrink-0">
                {VERDICT_LABELS[tradeEnvRollup.verdict]}
              </DenseTag>
              <span className="truncate text-muted-foreground">{tradeEnvRollup.cause}</span>
            </span>
            <span
              className="inline-flex min-w-0 max-w-full items-center gap-1"
              title={sharedRollup.cause}
            >
              <span className="text-muted-foreground shrink-0">Shared</span>
              <StatusLamp value={verdictLamp(sharedRollup.verdict)} kind="reach" />
              <DenseTag variant={verdictTag(sharedRollup.verdict)} className="text-[9px] shrink-0">
                {VERDICT_LABELS[sharedRollup.verdict]}
              </DenseTag>
              <span className="truncate text-muted-foreground">{sharedRollup.cause}</span>
            </span>
            <SystemGapMeta summary={systemGapSummary} />
            <span className="text-muted-foreground">{domainCountsLabel || '—'}</span>
            {system.referenceDomainCount > 0 ? (
              <span className="text-muted-foreground" title="Apollo planes with no runtime probe contract">
                {system.referenceDomainCount} reference
              </span>
            ) : null}
            {attentionQuiet ? (
              <span className="font-mono-tabular">
                alerts {system.firingAlerts} firing · {system.mappedFiringAlerts} mapped
              </span>
            ) : (
              <button
                type="button"
                className="font-mono-tabular text-warning hover:underline"
                title="Scroll to Attention"
                onClick={scrollToAttention}
              >
                alerts {system.firingAlerts} firing · {system.mappedFiringAlerts} mapped
              </button>
            )}
            <span className="font-mono-tabular">freshness {formatFreshness(system.freshnessMs)}</span>
            <span className="font-mono-tabular" title="Trade namespace for env-scoped probes">
              {namespace}
            </span>
            <span className="ml-auto">
              Layer B {viewModel.layerBStatus}
              {!viewModel.prometheusConfigured ? ' · Prometheus not configured' : ''}
            </span>
            {!viewModel.prometheusConfigured && !isLoading ? (
              <p className="m-0 w-full text-[var(--text-dense-caption)] text-muted-foreground">
                Missing scrape data is shown as UNKNOWN / NOT OBSERVED — never as HEALTHY. Install Layer B
                via Rocket → Cluster, then return here for system verdict.
              </p>
            ) : null}
          </>
        }
      />

      {/* Apollo Domain Health — Trade env lives on Satellite card; Grafana is per-domain */}
      <OpsSection
        title="Apollo Domain Health"
        description="Runtime domains in one row · Trade env on Satellite only · Grafana opens that domain’s catalog dashboard · Reference not probed"
        bodyPadding="compact"
        overflow="visible"
        collapsible={systemHealthy}
        defaultCollapsed={systemHealthy}
        actions={<SectionRefreshButton isFetching={isFetching} onClick={refetchAll} />}
        headerExtra={
          <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground" title={GAP_LEGEND}>
            {GAP_LEGEND}
          </p>
        }
      >
        <div className="flex flex-col gap-2.5">
          {runtimeDomainCards.length > 0 ? (
            <div className="flex flex-wrap gap-1.5">
              {runtimeDomainCards.map(d => (
                <DomainCard
                  key={d.domain}
                  domain={d}
                  selected={selectedDomain === d.domain}
                  onSelect={() => setSelectedDomain(d.domain)}
                  tradeEnv={d.envScope === 'env' && !lockToViewer ? tradeEnv : undefined}
                  onTradeEnvChange={d.envScope === 'env' && !lockToViewer ? setTradeEnv : undefined}
                  namespace={d.envScope === 'env' ? namespace : undefined}
                  grafana={primaryGrafanaForDomain(d.domain, viewModel.dashboards)}
                />
              ))}
            </div>
          ) : null}

          {referenceDomains.length > 0 ? (
            <div className="flex flex-col gap-1 border-t border-[var(--table-rule)] pt-2">
              <OpsSubsectionTitle>Reference domains (not probed)</OpsSubsectionTitle>
              <div className="flex flex-wrap gap-1.5">
                {referenceDomains.map(d => (
                  <ReferenceDomainChip
                    key={d.domain}
                    domain={d}
                    selected={selectedDomain === d.domain}
                    onSelect={() => setSelectedDomain(d.domain)}
                  />
                ))}
              </div>
            </div>
          ) : null}
        </div>
      </OpsSection>

      <ObservabilityAttentionPanel
        isLoading={isLoading}
        attentionQuiet={attentionQuiet}
        viewModelAttentionLength={viewModel.attention.length}
        filteredAttention={filteredAttention}
        attentionScope={attentionScope}
        setAttentionScope={setAttentionScope}
        mutePending={muteMutation.isPending}
        onMute={item => muteMutation.mutate(item)}
        muteMessage={muteMessage}
        activeMuteCount={activeMuteCount}
        canOperate={canOperate}
        attentionDetail={attentionDetail}
        setAttentionDetail={setAttentionDetail}
        muteConfirmItem={muteConfirmItem}
        setMuteConfirmItem={setMuteConfirmItem}
        onNavigate={onNavigate}
      />

      <ObservabilitySelectedDomain
        selected={selected}
        selectedDomainHealthy={selectedDomainHealthy}
        selectedRequiredSignals={selectedRequiredSignals}
        checkpointsQuiet={checkpointsQuiet}
        dependencyQuiet={dependencyQuiet}
        goldenQuiet={goldenQuiet}
        scrapeQuiet={scrapeQuiet}
        selectedPrimaryGrafana={selectedPrimaryGrafana}
        onNavigate={onNavigate}
      />


      {/* Grafana catalog — collapsed by default when system is healthy */}
      {systemHealthy ? (
        <details className="page-section panel-elevated overflow-hidden">
          <summary className="flex cursor-pointer list-none flex-wrap items-center gap-2 px-3 py-2.5 [&::-webkit-details-marker]:hidden">
            <span className="ops-section-title">Grafana dashboards</span>
            <span className="text-[var(--text-dense-caption)] text-muted-foreground">
              Deep evidence · {viewModel.dashboards.length} catalogued · expand when needed
            </span>
          </summary>
          <div className="border-t border-[var(--table-rule)]">
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
          </div>
        </details>
      ) : (
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
      )}

    </div>
  )
}
