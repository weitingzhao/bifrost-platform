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
import {
  Button,
  ConfirmDialog,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  SegmentControl,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  cn,
} from '@bifrost/ui'
import { Wrench } from 'lucide-react'
import { startRemediation } from '@/api/remediation'
import { postAttentionMute } from '@/api/telemetry'
import { TradeNsSegmentControl } from '@/components/TradeNsSegmentControl'
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
  SYSTEM_DOMAIN_ICON,
  SYSTEM_DOMAIN_VARIANT,
  type SystemDomainId,
} from '@/lib/architecture/systemDomainCatalog'
import type { TradeEnvId } from '@/lib/envVisual'
import type {
  AttentionItem,
  DomainHealth,
  EvaluatedSignal,
  GapSummary,
  GrafanaDashboardEntry,
  ObservabilityVerdict,
  SignalGap,
  SignalState,
} from '@/lib/observability'
import {
  ATTENTION_MUTE_DEFAULT_HOURS,
  attentionCtaActionLabel,
  buildAttentionBatchRemediationPrompt,
  buildAttentionRemediationPrompt,
  filterMutedAttention,
  largestAttentionBatchGroup,
  listActiveAttentionMutes,
  maxVerdict,
  muteAttentionIds,
  scopeForAttentionRemediation,
  signalStateToVerdict,
  signalToGap,
  sumGapSummaries,
  VERDICT_LABELS,
} from '@/lib/observability'

const GAP_LEGEND =
  'ok = matched · fail = unhealthy · blind = probe missing · by-design = optional contract · reference = plane not probed'

type AttentionScopeFilter = 'all' | 'trade_env' | 'shared'

const ATTENTION_SCOPE_OPTIONS: { value: AttentionScopeFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'trade_env', label: 'Trade env' },
  { value: 'shared', label: 'Shared' },
]

/** Worst verdict among participating runtime domains (ignores pure not_observed). */
function rollupDomainVerdict(domains: DomainHealth[]): {
  verdict: ObservabilityVerdict
  cause: string
} {
  const participating = domains.filter(d => d.verdict !== 'not_observed')
  if (domains.length === 0) {
    return { verdict: 'not_observed', cause: 'No domains in this plane' }
  }
  if (participating.length === 0) {
    return { verdict: 'not_observed', cause: 'No observed domains' }
  }
  let verdict: ObservabilityVerdict = 'healthy'
  for (const d of participating) {
    verdict = maxVerdict(verdict, d.verdict)
  }
  if (verdict === 'healthy') {
    return { verdict, cause: 'Healthy' }
  }
  const worst = participating.find(d => d.verdict === verdict)
  return {
    verdict,
    cause: worst != null ? `${worst.label}: ${worst.reason}` : VERDICT_LABELS[verdict],
  }
}

function attentionMatchesScope(item: AttentionItem, filter: AttentionScopeFilter): boolean {
  if (filter === 'all') return true
  if (filter === 'shared') return item.env === 'shared'
  return item.env !== 'shared'
}

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

const SIGNAL_STATE_LABELS: Record<SignalState, string> = {
  healthy: 'HEALTHY',
  degraded: 'DEGRADED',
  critical: 'CRITICAL',
  unknown: 'UNKNOWN',
  not_observed: 'NOT OBSERVED',
  expected_off: 'EXPECTED OFF',
}

const GAP_TAG_VARIANT: Record<SignalGap, 'success' | 'danger' | 'warning' | 'neutral'> = {
  ok: 'success',
  fail: 'danger',
  blind: 'warning',
  by_design: 'neutral',
}

const GAP_LABEL: Record<SignalGap, string> = {
  ok: 'ok',
  fail: 'fail',
  blind: 'blind',
  by_design: 'by-design',
}

function gapPartClass(gap: SignalGap): string {
  switch (gap) {
    case 'ok':
      return 'text-success'
    case 'fail':
      return 'text-danger'
    case 'blind':
      return 'text-warning'
    default:
      return 'text-muted-foreground'
  }
}

/** Domain card line: "4/4 ok" when all ok; else "1 ok · 1 fail · 2 blind" (by_design in tooltip). */
function formatGapSummaryLine(g: GapSummary): { line: string; title: string } {
  const title = `${g.ok} ok · ${g.fail} fail · ${g.blind} blind · ${g.byDesign} by-design · ${g.total} required`
  if (g.total === 0) return { line: '0 required', title }
  if (g.ok === g.total) return { line: `${g.ok}/${g.total} ok`, title }
  const parts: string[] = []
  if (g.ok > 0) parts.push(`${g.ok} ok`)
  if (g.fail > 0) parts.push(`${g.fail} fail`)
  if (g.blind > 0) parts.push(`${g.blind} blind`)
  // by_design omitted from primary line — surface via title tooltip
  return { line: parts.length > 0 ? parts.join(' · ') : `${g.byDesign} by-design`, title }
}

function GapSummaryText({
  summary,
  className,
}: {
  summary: GapSummary
  className?: string
}) {
  const { line, title } = formatGapSummaryLine(summary)
  const allOk = summary.total > 0 && summary.ok === summary.total
  return (
    <span className={cn('font-mono-tabular', className)} title={title}>
      {allOk ? (
        <span className={gapPartClass('ok')}>{line}</span>
      ) : (
        line.split(' · ').map((part, i) => {
          const gap: SignalGap =
            part.includes('fail')
              ? 'fail'
              : part.includes('blind')
                ? 'blind'
                : part.includes('by-design')
                  ? 'by_design'
                  : 'ok'
          return (
            <span key={part}>
              {i > 0 ? ' · ' : null}
              <span className={gapPartClass(gap)}>{part}</span>
            </span>
          )
        })
      )}
    </span>
  )
}

type DomainGrafanaLink = { label: string; url: string }

/** Primary catalog dashboard for a domain (card shortcut). */
function primaryGrafanaForDomain(
  domain: SystemDomainId,
  dashboards: Array<GrafanaDashboardEntry & { available: boolean; url: string | null }>,
): DomainGrafanaLink | null {
  const hit = dashboards.find(d => d.domain === domain && d.available && d.url != null)
  if (hit?.url == null) return null
  return { label: hit.title, url: hit.url }
}

function DomainCard({
  domain,
  selected,
  onSelect,
  tradeEnv,
  onTradeEnvChange,
  namespace,
  grafana,
}: {
  domain: DomainHealth
  selected: boolean
  onSelect: () => void
  /** Satellite only — Trade NS lives on the card, not a page toolbar. */
  tradeEnv?: TradeEnvId
  onTradeEnvChange?: (env: TradeEnvId) => void
  namespace?: string
  /** Domain-primary Grafana deep link (catalog). */
  grafana?: DomainGrafanaLink | null
}) {
  const Icon = SYSTEM_DOMAIN_ICON[domain.domain]
  const tradeScoped =
    domain.envScope === 'env' && tradeEnv != null && onTradeEnvChange != null
  return (
    <div
      className={cn(
        'flex min-w-[10.5rem] flex-1 flex-col gap-1 rounded-md border px-2.5 py-2 transition-colors',
        selected
          ? 'border-[var(--ring)] bg-[var(--accent)]'
          : 'border-[var(--border)] bg-[var(--secondary)] hover:bg-[var(--accent)]/60',
      )}
    >
      <button
        type="button"
        onClick={onSelect}
        title={
          tradeScoped
            ? `${domain.label} · Trade env scopes this domain (${namespace ?? tradeEnv})`
            : domain.envScope === 'mixed'
              ? `${domain.label} · mixed env scope`
              : `${domain.label} · shared platform (not scoped by Trade env)`
        }
        className="flex flex-col gap-1 text-left"
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
        <span
          className="line-clamp-2 text-[var(--text-dense-caption)] text-muted-foreground"
          title={domain.reason}
        >
          {domain.reason}
        </span>
        <span className="text-[var(--text-dense-caption)]">
          <GapSummaryText summary={domain.gapSummary} />
          {domain.envScope === 'mixed' ? (
            <span className="text-muted-foreground"> · mixed</span>
          ) : null}
        </span>
      </button>

      {tradeScoped ? (
        <div className="flex flex-col gap-1 border-t border-[var(--border)]/70 pt-1.5">
          <span
            className="text-[9px] font-medium uppercase tracking-wide text-muted-foreground"
            title="Scopes Satellite probes / bus-deep only"
          >
            Trade env · {namespace}
          </span>
          <TradeNsSegmentControl
            value={tradeEnv}
            onChange={onTradeEnvChange}
            size="xs"
            ariaLabel="Satellite Trade environment"
          />
        </div>
      ) : null}

      <div className="mt-auto flex items-center border-t border-[var(--border)]/70 pt-1.5">
        {grafana != null ? (
          <a
            href={grafana.url}
            target="_blank"
            rel="noreferrer"
            title={`Open Grafana · ${grafana.label}`}
            className="text-[var(--text-dense-caption)] text-primary underline-offset-2 hover:underline"
            onClick={e => e.stopPropagation()}
          >
            Grafana
          </a>
        ) : (
          <span
            className="text-[var(--text-dense-caption)] text-muted-foreground"
            title="No deployed Grafana dashboard for this domain yet (catalog uid unset)"
          >
            Grafana · not deployed
          </span>
        )}
      </div>
    </div>
  )
}

/** Compact selectable chip for Apollo reference planes (no runtime probe contract). */
function ReferenceDomainChip({
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
      title={`${domain.label} — by design · no runtime contract`}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-left transition-colors',
        selected
          ? 'border-[var(--ring)] bg-[var(--accent)]'
          : 'border-[var(--border)] bg-[var(--secondary)]/70 hover:bg-[var(--accent)]/50',
      )}
    >
      <Icon className="size-3 shrink-0 text-muted-foreground" aria-hidden />
      <span className="text-[var(--text-dense-caption)] font-medium">{domain.label}</span>
      <DenseTag variant="neutral" className="text-[9px]">
        reference
      </DenseTag>
      <span className="text-[var(--text-dense-caption)] text-muted-foreground">
        by design · no runtime contract
      </span>
    </button>
  )
}

function checkpointExpect(s: EvaluatedSignal): string {
  return s.def.optionalContract === true ? 'NOT OBSERVED (by design)' : 'HEALTHY'
}

/** Colored system-level gap meta: fail · blind · by-design · ok */
function SystemGapMeta({ summary }: { summary: GapSummary }) {
  const parts: { gap: SignalGap; text: string }[] = []
  if (summary.fail > 0) parts.push({ gap: 'fail', text: `${summary.fail} fail` })
  if (summary.blind > 0) parts.push({ gap: 'blind', text: `${summary.blind} blind` })
  if (summary.byDesign > 0) parts.push({ gap: 'by_design', text: `${summary.byDesign} by-design` })
  if (summary.ok > 0) parts.push({ gap: 'ok', text: `${summary.ok} ok` })
  if (parts.length === 0) return <span>—</span>
  return (
    <span
      className="font-mono-tabular"
      title={`${summary.ok} ok · ${summary.fail} fail · ${summary.blind} blind · ${summary.byDesign} by-design · ${summary.total} required`}
    >
      {parts.map((p, i) => (
        <span key={p.text}>
          {i > 0 ? ' · ' : null}
          <span className={gapPartClass(p.gap)}>{p.text}</span>
        </span>
      ))}
    </span>
  )
}

export function ObservabilityPage({
  onNavigate,
  ambientJobId,
  onStartAgentJob,
}: {
  onNavigate?: (tab: string) => void
  ambientJobId?: string | null
  onStartAgentJob?: (job: AmbientAgentJob) => void
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
  const system = viewModel.system
  const selected = viewModel.selected

  const agentBlockedReason = ambientAgentBlockedReason(
    canOperate,
    ambientJobId,
    onStartAgentJob,
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

      {/* Attention */}
      <OpsSection
        id="obs-attention"
        title="Attention"
        description="Severity · Domain · Environment · Signal · Since · Owner · Action — Inspect / Agent Fix / Mute 2h (not a fix)"
        bodyPadding="none"
        overflow="hidden"
        collapsible={attentionQuiet}
        defaultCollapsed={attentionQuiet}
        actions={
          viewModel.attention.length > 0 ? (
            <div className="flex flex-wrap items-center gap-2">
              {batchGroup != null && (
                <Button
                  size="sm"
                  variant="outline"
                  disabled={agentBlockedReason != null || remediationMutation.isPending}
                  title={
                    agentBlockedReason ??
                    `Batch Agent Fix for ${batchGroup.items.length}× ${batchGroup.playbookId} (Operator Dock)`
                  }
                  onClick={() => setBatchConfirmOpen(true)}
                >
                  <Wrench size={14} className="mr-1" aria-hidden />
                  Fix {batchGroup.items.length}× shared
                </Button>
              )}
              <SegmentControl
                size="sm"
                value={attentionScope}
                options={ATTENTION_SCOPE_OPTIONS}
                onChange={v => setAttentionScope(v as AttentionScopeFilter)}
                ariaLabel="Attention scope"
              />
            </div>
          ) : null
        }
        headerExtra={
          remediationError != null ||
          lastRemediationJobId != null ||
          muteMessage != null ||
          activeMuteCount > 0 ? (
            <p className="m-0 text-[var(--text-dense-caption)]">
              {remediationError != null ? (
                <span className="text-danger">{remediationError}</span>
              ) : null}
              {remediationError == null && lastRemediationJobId != null ? (
                <span className="text-muted-foreground">
                  Agent task started · Expand Operator Dock · job {lastRemediationJobId}
                </span>
              ) : null}
              {muteMessage != null ? (
                <span className="text-muted-foreground">
                  {remediationError != null || lastRemediationJobId != null ? ' · ' : null}
                  {muteMessage}
                </span>
              ) : null}
              {activeMuteCount > 0 ? (
                <span className="text-muted-foreground">
                  {(remediationError != null ||
                    lastRemediationJobId != null ||
                    muteMessage != null) &&
                    ' · '}
                  {activeMuteCount} muted (UI{canOperate ? ' ± AM' : ''} · not fixed)
                </span>
              ) : null}
            </p>
          ) : null
        }
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
            ) : filteredAttention.length === 0 ? (
              <DenseTableRow>
                <DenseTableCell colSpan={7} className="text-muted-foreground">
                  No attention items in this scope — try All or another filter.
                </DenseTableCell>
              </DenseTableRow>
            ) : (
              filteredAttention.map(item => (
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
                    <span className="inline-flex flex-wrap items-center gap-1.5">
                      <button
                        type="button"
                        className="focus-strip-link text-[var(--text-dense-caption)]"
                        onClick={() => setAttentionDetail(item)}
                      >
                        Inspect
                      </button>
                      <button
                        type="button"
                        className="focus-strip-link text-[var(--text-dense-caption)] text-muted-foreground"
                        title="Mute 2h in Observability (optional Alertmanager silence) — not a root-cause fix"
                        onClick={() => setMuteConfirmItem(item)}
                      >
                        Mute
                      </button>
                      {item.triage.cta === 'manual' ? (
                        <Button
                          variant="ghost"
                          size="xs"
                          className="h-6 px-1.5"
                          onClick={() => {
                            if (item.triage.detailRoute != null) {
                              onNavigate?.(item.triage.detailRoute)
                            } else {
                              setAttentionDetail(item)
                            }
                          }}
                          title={item.triage.suggestedAction}
                        >
                          Manual
                        </Button>
                      ) : (
                        <Button
                          variant="ghost"
                          size="xs"
                          className="h-6 px-1.5"
                          disabled={
                            agentBlockedReason != null || remediationMutation.isPending
                          }
                          title={
                            agentBlockedReason ??
                            `${attentionCtaActionLabel(item.triage.cta)} · ${item.triage.trackReason}`
                          }
                          onClick={() => runAttentionRemediation(item)}
                        >
                          <Wrench size={12} className="mr-1" aria-hidden />
                          {attentionCtaActionLabel(item.triage.cta)}
                        </Button>
                      )}
                    </span>
                  </DenseTableCell>
                </DenseTableRow>
              ))
            )}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

      {/* Selected Domain — primary place for domain Grafana (all catalogued dashboards) */}
      <OpsSection
        title={`Selected Domain · ${selected.domain}`}
        description="Checkpoints · dependency path · golden signals · scrape targets · domain Grafana"
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
        <div className="flex flex-col gap-3">
          <div>
            <OpsSubsectionTitle className="mb-1">Checkpoints</OpsSubsectionTitle>
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
          </div>

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
                    [
                      'Remediation track',
                      `${attentionDetail.triage.track}${
                        attentionDetail.triage.playbookId != null
                          ? ` · ${attentionDetail.triage.playbookId}`
                          : ''
                      } — ${attentionDetail.triage.trackReason}`,
                    ],
                    ['Suggested action', attentionDetail.triage.suggestedAction],
                  ] as const
                ).map(([label, value]) => (
                  <div key={label}>
                    <p className="m-0 mb-0.5 text-[var(--text-dense-caption)] font-medium text-muted-foreground">
                      {label}
                    </p>
                    <p className="m-0 text-[var(--text-dense-meta)]">{value}</p>
                  </div>
                ))}
                {agentBlockedReason != null && attentionDetail.triage.cta !== 'manual' && (
                  <p className="m-0 text-[var(--text-dense-caption)] text-warning">
                    {agentBlockedReason}
                  </p>
                )}
                {remediationError != null && (
                  <p className="m-0 text-[var(--text-dense-caption)] text-danger">{remediationError}</p>
                )}
                <div className="flex flex-wrap gap-2 border-t border-[var(--border)] pt-2">
                  {attentionDetail.triage.cta === 'agent_fix' && (
                    <Button
                      size="sm"
                      disabled={agentBlockedReason != null || remediationMutation.isPending}
                      title={agentBlockedReason ?? 'Start assisted Agent Fix in Operator Dock'}
                      onClick={() => runAttentionRemediation(attentionDetail)}
                    >
                      <Wrench size={14} className="mr-1" aria-hidden />
                      Agent Fix
                    </Button>
                  )}
                  {attentionDetail.triage.cta === 'diagnose' && (
                    <Button
                      size="sm"
                      disabled={agentBlockedReason != null || remediationMutation.isPending}
                      title={agentBlockedReason ?? 'Start assisted diagnose in Operator Dock'}
                      onClick={() => runAttentionRemediation(attentionDetail)}
                    >
                      <Wrench size={14} className="mr-1" aria-hidden />
                      Diagnose
                    </Button>
                  )}
                  {attentionDetail.triage.cta === 'manual' && (
                    <Button
                      size="sm"
                      variant="default"
                      onClick={() => {
                        if (attentionDetail.triage.detailRoute != null) {
                          onNavigate?.(attentionDetail.triage.detailRoute)
                        }
                        setAttentionDetail(null)
                      }}
                    >
                      Manual next
                    </Button>
                  )}
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
                    <Button size="sm" variant="outline" asChild>
                      <a href={attentionDetail.triage.grafanaUrl} target="_blank" rel="noreferrer">
                        Open Grafana
                      </a>
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={muteMutation.isPending}
                    title="Mute 2h — UI suppress + audit; optional Alertmanager silence. Not a fix."
                    onClick={() => setMuteConfirmItem(attentionDetail)}
                  >
                    Mute 2h
                  </Button>
                </div>
              </div>
            </>
          )}
        </SheetContent>
      </Sheet>

      <ConfirmDialog
        open={muteConfirmItem != null}
        title="Mute Attention item for 2 hours?"
        message="This hides the row in Observability and may create an Alertmanager silence when configured. Mute is not a root-cause fix — alerts can return when the mute expires."
        confirmLabel="Mute 2h"
        confirming={muteMutation.isPending}
        onConfirm={() => {
          if (muteConfirmItem != null) muteMutation.mutate(muteConfirmItem)
        }}
        onCancel={() => setMuteConfirmItem(null)}
      />

      <ConfirmDialog
        open={batchConfirmOpen && batchGroup != null}
        title={
          batchGroup != null
            ? `Batch Agent Fix (${batchGroup.items.length}× ${batchGroup.playbookId})?`
            : 'Batch Agent Fix?'
        }
        message="Starts one assisted remediation job in Operator Dock covering all matching Attention rows. Approve actuations in the dock — no auto-remediate."
        confirmLabel="Start batch Fix"
        confirming={remediationMutation.isPending}
        onConfirm={() => {
          if (batchGroup == null || agentBlockedReason != null) return
          remediationMutation.mutate({
            batchPlaybookId: batchGroup.playbookId,
            batchPrompt: buildAttentionBatchRemediationPrompt(batchGroup),
          })
        }}
        onCancel={() => setBatchConfirmOpen(false)}
      />
    </div>
  )
}
