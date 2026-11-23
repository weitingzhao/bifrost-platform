/**
 * Mission Control → Observability
 * One-screen answer: “Is the whole system healthy right now?”
 * Grafana is deep evidence — not a duplicated dashboard gallery.
 *
 * Attention: triage entry only — Inspect / Mute / Manual next. Agents are not
 * started from this page.
 */

import { useState } from 'react'
import { useObservabilityPageProjection } from '@/domains/foundation/observability/useObservabilityPageProjection'
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
  muteAttentionIds,
  VERDICT_LABELS,
} from '@/lib/observability'
import { DomainCard } from '@/pages/observability/DomainCard'
import { ObservabilityAttentionPanel } from '@/pages/observability/ObservabilityAttentionPanel'
import { ObservabilitySelectedDomain } from '@/pages/observability/ObservabilitySelectedDomain'
import { ReferenceDomainChip } from '@/pages/observability/ReferenceDomainChip'
import { SystemGapMeta } from '@/pages/observability/SystemGapMeta'
import {
  formatFreshness,
  GAP_LEGEND,
  primaryGrafanaForDomain,
  verdictLamp,
  verdictTag,
  type AttentionScopeFilter,
} from '@/pages/observability/observabilityFormat'

function alertsMetaLabel(s: { firingAlerts: number; pendingAlerts: number; mappedFiringAlerts: number }): string {
  // Same firing number as the frame's corner; pending and the verdict's share are named, not added in.
  return [`alerts ${s.firingAlerts} firing`, s.pendingAlerts > 0 ? `${s.pendingAlerts} pending` : null, `${s.mappedFiringAlerts} move the verdict`].filter(Boolean).join(' · ')
}

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

  const {
    runtimeDomainCards,
    firingOffDomainCards,
    referenceDomains,
    tradeEnvRollup,
    sharedRollup,
    filteredAttention,
    activeMuteCount,
    domainCountsLabel,
    systemGapSummary,
    selectedRequiredSignals,
    selectedDomainHealthy,
    checkpointsQuiet,
    dependencyQuiet,
    goldenQuiet,
    scrapeQuiet,
    systemHealthy,
    selectedPrimaryGrafana,
    attentionQuiet,
  } = useObservabilityPageProjection(viewModel, selectedDomain, attentionScope, muteRevision, isLoading)

  const scrollToAttention = () => {
    document.getElementById('obs-attention')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  return (
    <div className="flex flex-col gap-3">
      <OpsVerdictStrip
        ariaLabel="System verdict"
        title={`SYSTEM VERDICT · ${tradeEnv.toUpperCase()}`}
        lamp={verdictLamp(system.overall)}
        tagLabel={isLoading ? '…' : system.label}
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
                {alertsMetaLabel(system)}
              </span>
            ) : (
              <button
                type="button"
                className="font-mono-tabular text-warning hover:underline"
                title="Scroll to Attention"
                onClick={scrollToAttention}
              >
                {alertsMetaLabel(system)}
              </button>
            )}
            <span className="font-mono-tabular">freshness {formatFreshness(system.freshnessMs)}</span>
            <span className="font-mono-tabular" title="Trade namespace for env-scoped probes">
              {namespace}
            </span>
            <span className="ml-auto">
              {/* Plain words (brand item 13, 2026-11-23): "Layer B" is the monitoring stack (Prometheus, Grafana). */}
              <span title="Layer B: the cluster's monitoring stack">Monitoring stack {viewModel.layerBStatus}</span>
              {!viewModel.prometheusConfigured ? ' · Prometheus not configured' : ''}
            </span>
            {!viewModel.prometheusConfigured && !isLoading ? (
              <p className="m-0 w-full text-[var(--text-dense-caption)] text-muted-foreground">
                Missing scrape data is shown as unknown or boundary — never as ok. Install the monitoring stack
                via Rocket → Cluster, then come back here for the system verdict.
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

          {firingOffDomainCards.length > 0 ? (
            // The cards' firing chips plus this line make the frame's total (箭10 2026-11-23).
            <details className="text-[var(--text-dense-meta)]" data-firing-off-cards>
              <summary className="cursor-pointer text-muted-foreground">
                {firingOffDomainCards.length} firing not on any domain card
              </summary>
              <ul className="m-0 mt-1 flex list-none flex-col gap-0.5 p-0">
                {firingOffDomainCards.map(a => (
                  <li key={a.id} className="min-w-0 truncate" title={a.summary}>
                    <span className="font-medium">{a.name}</span>
                    <span className="text-muted-foreground">
                      {` · ${[a.labels.severity, a.domain ?? 'no domain', a.labels.namespace, a.summary].filter(Boolean).join(' · ')}`}
                    </span>
                  </li>
                ))}
              </ul>
            </details>
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
