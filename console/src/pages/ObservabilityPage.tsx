/**
 * Mission Control → Observability
 * One-screen answer: “Is the whole system healthy right now?”
 * Grafana is deep evidence — not a duplicated dashboard gallery.
 *
 * Attention remediation: triage entry only — Agent Fix / Diagnose reuse
 * startRemediation + ambient Operator Dock (no second execution engine).
 */

import { useCallback, useMemo, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Button, DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag } from '@bifrost/ui'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { startRemediation } from '@/api/remediation'
import { postAttentionMute } from '@/api/telemetry'
import type { OpenAgentDeskArg } from '@/lib/agent/openAgentDesk'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { StatusLamp } from '@/components/StatusLamp'
import { useObservabilitySnapshot } from '@/hooks/useObservabilitySnapshot'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  ambientAgentBlockedReason,
  type AmbientAgentJob,
} from '@/lib/agent/ambientAgent'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import {
  SYSTEM_DOMAIN_VARIANT,
  type SystemDomainId,
} from '@/lib/architecture/systemDomainCatalog'
import type { AttentionItem } from '@/lib/observability'
import {
  ATTENTION_MUTE_DEFAULT_HOURS,
  buildAttentionBatchRemediationPrompt,
  buildAttentionRemediationPrompt,
  buildObservabilityAgentPack,
  buildObservabilityDiagnosePrefill,
  filterMutedAttention,
  largestAttentionBatchGroup,
  listActiveAttentionMutes,
  muteAttentionIds,
  scopeForAttentionRemediation,
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
  ambientJobId,
  ambientJobStatus,
  onStartAgentJob,
  onOpenAgentDesk,
}: {
  onNavigate?: (tab: string) => void
  ambientJobId?: string | null
  ambientJobStatus?: AmbientAgentJob['status'] | null
  onStartAgentJob?: (job: AmbientAgentJob) => void
  onOpenAgentDesk?: (arg: OpenAgentDeskArg) => void
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
  const { canOperate } = usePlatformAuth()
  const qc = useQueryClient()

  const [attentionDetail, setAttentionDetail] = useState<AttentionItem | null>(null)
  const [attentionScope, setAttentionScope] = useState<AttentionScopeFilter>('all')
  const [lastRemediationJobId, setLastRemediationJobId] = useState<string | null>(null)
  const [remediationError, setRemediationError] = useState<string | null>(null)
  const [muteRevision, setMuteRevision] = useState(0)
  const [muteConfirmItem, setMuteConfirmItem] = useState<AttentionItem | null>(null)
  const [batchConfirmOpen, setBatchConfirmOpen] = useState(false)
  const [muteMessage, setMuteMessage] = useState<string | null>(null)
  const [copyState, setCopyState] = useState<'idle' | 'busy' | 'copied' | 'error'>('idle')
  const [diagnoseBusy, setDiagnoseBusy] = useState(false)
  const system = viewModel.system
  const selected = viewModel.selected

  const agentBlockedReason = ambientAgentBlockedReason(
    canOperate,
    ambientJobId,
    onStartAgentJob,
    ambientJobStatus,
  )

  const invalidateObservability = useCallback(() => {
    void qc.invalidateQueries({ queryKey: ['telemetry', 'alerts'] })
    void qc.invalidateQueries({ queryKey: ['telemetry', 'targets'] })
    void qc.invalidateQueries({ queryKey: ['telemetry', 'overview'] })
    void qc.invalidateQueries({ queryKey: ['cluster', 'observability'] })
    void qc.invalidateQueries({ queryKey: ['cluster'] })
    void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
  }, [qc])

  const remediationMutation = useMutation({
    mutationFn: ({
      item,
      batchPrompt,
      batchPlaybookId,
    }: {
      item?: AttentionItem
      batchPrompt?: string
      batchPlaybookId?: string
    }) => {
      if (batchPrompt != null && batchPlaybookId != null) {
        const scope = scopeForAttentionRemediation(batchPlaybookId)
        return startRemediation({ scope, prompt: batchPrompt })
      }
      if (item == null) throw new Error('missing attention item')
      const scope = scopeForAttentionRemediation(item.triage.playbookId)
      const prompt = buildAttentionRemediationPrompt(item)
      return startRemediation({ scope, prompt })
    },
    onSuccess: (job, vars) => {
      setRemediationError(null)
      setLastRemediationJobId(job.id)
      const playbookId = vars.batchPlaybookId ?? vars.item?.triage.playbookId
      const scope = scopeForAttentionRemediation(playbookId)
      onStartAgentJob?.({ id: job.id, scope, label: scopeToLabel(scope) })
      invalidateObservability()
      setAttentionDetail(null)
      setBatchConfirmOpen(false)
    },
    onError: (err: Error) => {
      setRemediationError(err.message)
    },
  })

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

  const runAttentionRemediation = useCallback(
    (item: AttentionItem) => {
      if (agentBlockedReason != null) return
      if (item.triage.cta === 'manual') {
        if (item.triage.detailRoute != null) onNavigate?.(item.triage.detailRoute)
        return
      }
      remediationMutation.mutate({ item })
    },
    [agentBlockedReason, onNavigate, remediationMutation],
  )

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

  const batchGroup = useMemo(
    () => largestAttentionBatchGroup(filteredAttention),
    [filteredAttention],
  )

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

  const packCtx = useMemo(
    () => ({
      generatedAt: new Date().toISOString(),
      tradeEnv,
      namespace,
      selectedDomain,
      viewModel,
    }),
    [namespace, selectedDomain, tradeEnv, viewModel],
  )

  async function handleCopyForAgent() {
    if (copyState === 'busy') return
    setCopyState('busy')
    try {
      await navigator.clipboard.writeText(buildObservabilityAgentPack(packCtx))
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }

  function handleDiagnoseWithAgent() {
    if (diagnoseBusy || onOpenAgentDesk == null) return
    setDiagnoseBusy(true)
    onOpenAgentDesk({ prefill: buildObservabilityDiagnosePrefill(packCtx) })
    window.setTimeout(() => setDiagnoseBusy(false), 400)
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
          <div className="flex flex-wrap items-center gap-1.5">
            <Button
              variant="outline"
              size="sm"
              disabled={copyState === 'busy' || isLoading}
              title="Copy a repair pack (verdict + domains + Attention + Alerts) for an AI agent"
              onClick={() => void handleCopyForAgent()}
            >
              {copyState === 'busy'
                ? 'Exporting…'
                : copyState === 'copied'
                  ? 'Copied!'
                  : copyState === 'error'
                    ? 'Copy failed'
                    : 'Copy for Agent'}
            </Button>
            {onOpenAgentDesk != null ? (
              <AgentTriggerButton
                label="Diagnose with Agent"
                size="sm"
                pending={diagnoseBusy}
                disabled={isLoading}
                title="Open Agent Desk with Observability diagnose prefill (includes firing alerts)"
                onClick={handleDiagnoseWithAgent}
              />
            ) : null}
          </div>
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
                  tradeEnv={d.envScope === 'env' ? tradeEnv : undefined}
                  onTradeEnvChange={d.envScope === 'env' ? setTradeEnv : undefined}
                  namespace={d.envScope === 'env' ? namespace : undefined}
                  grafana={primaryGrafanaForDomain(d.domain, viewModel.dashboards)}
                />
              ))}
            </div>
          ) : null}

          {referenceDomains.length > 0 ? (
            <div className="flex flex-col gap-1 border-t border-[var(--border)] pt-2">
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
        batchGroup={batchGroup}
        agentBlockedReason={agentBlockedReason}
        remediationPending={remediationMutation.isPending}
        mutePending={muteMutation.isPending}
        onBatchRemediate={group => {
          remediationMutation.mutate({
            batchPlaybookId: group.playbookId,
            batchPrompt: buildAttentionBatchRemediationPrompt(group),
          })
        }}
        onMute={item => muteMutation.mutate(item)}
        remediationError={remediationError}
        lastRemediationJobId={lastRemediationJobId}
        muteMessage={muteMessage}
        activeMuteCount={activeMuteCount}
        canOperate={canOperate}
        attentionDetail={attentionDetail}
        setAttentionDetail={setAttentionDetail}
        muteConfirmItem={muteConfirmItem}
        setMuteConfirmItem={setMuteConfirmItem}
        batchConfirmOpen={batchConfirmOpen}
        setBatchConfirmOpen={setBatchConfirmOpen}
        onNavigate={onNavigate}
        runAttentionRemediation={runAttentionRemediation}
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
          <div className="border-t border-border">
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
