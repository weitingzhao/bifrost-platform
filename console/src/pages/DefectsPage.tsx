import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { PageHeader } from '@bifrost/ui'
import {
  DenseDataTable,
  DenseTableHeader,
  DenseTableBody,
  DenseTableHeadRow,
  DenseTableHead,
  DenseTableRow,
  DenseTableCell,
  DenseTag,
  StatusLamp,
  Button,
} from '@bifrost/ui'
import { AlertCircle, RefreshCw, TrendingUp, TrendingDown, Minus, ChevronDown, ChevronRight, Wrench } from 'lucide-react'
import { useState } from 'react'
import { fetchRetrospectiveReport, startRemediation } from '@/api/platform'
import { buildDefectPatternRemediatePrompt } from '@/lib/agent/defectPatternRemediatePrompt'
import { DEFECT_PATTERN_REMEDIATE_SCOPE } from '@/lib/agent/agentScopes'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import type { AmbientAgentJob } from '@/lib/agent/ambientAgent'
import { OpsSection } from '@/components/layout/OpsSection'
import type {
  RetrospectiveReport,
  RetrospectivePatternCluster,
  RetrospectiveRootCauseDistribution,
  RetrospectiveScopeStats,
  RetrospectiveToolUsage,
  RetrospectiveNamespaceActivity,
  RetrospectiveSeverity,
  RetrospectiveRootCause,
} from '@/api/types'

function severityVariant(s: RetrospectiveSeverity) {
  switch (s) {
    case 'critical': return 'danger' as const
    case 'high': return 'warning' as const
    case 'medium': return 'category' as const
    default: return 'category' as const
  }
}

function rootCauseLabel(r: string) {
  switch (r) {
    case 'transient': return 'Transient'
    case 'probe_drift': return 'Probe drift'
    case 'platform_defect': return 'Platform defect'
    case 'config_drift': return 'Config drift'
    case 'resource_limit': return 'Resource limit'
    case 'external': return 'External'
    default: return 'Unknown'
  }
}

function rootCauseColor(r: RetrospectiveRootCause) {
  switch (r) {
    case 'platform_defect': return 'bg-red-500/80'
    case 'probe_drift': return 'bg-orange-500/80'
    case 'config_drift': return 'bg-amber-500/80'
    case 'resource_limit': return 'bg-purple-500/80'
    case 'external': return 'bg-blue-500/80'
    case 'transient': return 'bg-emerald-500/80'
    default: return 'bg-zinc-500/80'
  }
}

function confidenceBar(confidence: number) {
  const pct = Math.round(confidence * 100)
  const color =
    confidence >= 0.7 ? 'bg-emerald-500/70' : confidence >= 0.4 ? 'bg-amber-500/70' : 'bg-zinc-500/70'
  return (
    <div className="flex items-center gap-1.5">
      <div className="w-12 h-1.5 rounded-full bg-secondary overflow-hidden">
        <div className={`h-full rounded-full ${color}`} style={{ width: `${pct}%` }} />
      </div>
      <span className="text-dense-caption font-mono tabular-nums">{pct}%</span>
    </div>
  )
}

function TrendIcon({ trend }: { trend: string }) {
  if (trend === 'up') return <TrendingUp className="h-3.5 w-3.5 text-red-400" />
  if (trend === 'down') return <TrendingDown className="h-3.5 w-3.5 text-emerald-400" />
  return <Minus className="h-3.5 w-3.5 text-muted-foreground" />
}

function StatsCards({ report }: { report: RetrospectiveReport }) {
  const score = report.health_score ?? 0
  const lampValue =
    score >= 90 ? 'ok' : score >= 70 ? 'degraded' : ('fail' as const)
  const scoreColor =
    score >= 90
      ? 'text-emerald-400'
      : score >= 70
        ? 'text-yellow-400'
        : 'text-red-400'

  return (
    <div className="grid grid-cols-4 gap-3">
      <div className="panel-elevated rounded-md p-4 flex items-center gap-3">
        <StatusLamp value={lampValue} />
        <div>
          <p className="text-dense-meta text-muted-foreground">Health Score</p>
          <p className={`text-2xl font-bold tabular-nums ${scoreColor}`}>
            {score.toFixed(0)}
          </p>
        </div>
      </div>
      <div className="panel-elevated rounded-md p-4">
        <p className="text-dense-meta text-muted-foreground">Total Jobs</p>
        <p className="text-2xl font-bold tabular-nums">{report.total_jobs}</p>
        <p className="text-dense-caption text-muted-foreground mt-1">{report.analysis_window}</p>
      </div>
      <div className="panel-elevated rounded-md p-4">
        <p className="text-dense-meta text-muted-foreground">Patterns Detected</p>
        <p className="text-2xl font-bold tabular-nums">{report.patterns?.length ?? 0}</p>
        <p className="text-dense-caption text-muted-foreground mt-1">
          {(report.patterns ?? []).filter(p => p.trending === 'up').length} trending up
        </p>
      </div>
      <div className="panel-elevated rounded-md p-4">
        <p className="text-dense-meta text-muted-foreground">Namespaces</p>
        <p className="text-2xl font-bold tabular-nums">{report.namespaces?.length ?? 0}</p>
        <p className="text-dense-caption text-muted-foreground mt-1">
          {report.tool_usage?.length ?? 0} tools used
        </p>
      </div>
    </div>
  )
}

function RootCauseDistBar({ dist }: { dist: RetrospectiveRootCauseDistribution[] }) {
  if (!dist || dist.length === 0) return null
  return (
    <OpsSection title="Root Cause Distribution">
      <div className="px-3 py-2 space-y-2">
        <div className="flex h-4 rounded overflow-hidden">
          {dist.map(d => (
            <div
              key={d.cause}
              className={`${rootCauseColor(d.cause)} first:rounded-l last:rounded-r`}
              style={{ width: `${d.fraction * 100}%` }}
              title={`${rootCauseLabel(d.cause)}: ${d.count} (${Math.round(d.fraction * 100)}%)`}
            />
          ))}
        </div>
        <div className="flex flex-wrap gap-x-4 gap-y-1">
          {dist.map(d => (
            <div key={d.cause} className="flex items-center gap-1.5 text-dense-caption">
              <div className={`w-2.5 h-2.5 rounded-sm ${rootCauseColor(d.cause)}`} />
              <span className="text-muted-foreground">{rootCauseLabel(d.cause)}</span>
              <span className="font-mono tabular-nums">{d.count}</span>
              <span className="text-muted-foreground">({Math.round(d.fraction * 100)}%)</span>
            </div>
          ))}
        </div>
      </div>
    </OpsSection>
  )
}

function InsightsPanel({ insights }: { insights: string[] | undefined }) {
  if ((insights?.length ?? 0) === 0) return null
  return (
    <OpsSection title="Insights">
      <ul className="space-y-1.5 px-3 py-2">
        {(insights ?? []).map((insight, i) => (
          <li key={i} className="text-dense-body text-muted-foreground">
            {insight}
          </li>
        ))}
      </ul>
    </OpsSection>
  )
}

function PatternsTable({
  patterns,
  onFixPattern,
  fixPending,
  canFix,
}: {
  patterns: RetrospectivePatternCluster[]
  onFixPattern?: (pattern: RetrospectivePatternCluster) => void
  fixPending?: boolean
  canFix?: boolean
}) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const toggle = (id: string) =>
    setExpanded(prev => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })

  if (patterns.length === 0) {
    return (
      <OpsSection title="Recurring Patterns">
        <p className="p-6 text-center text-muted-foreground text-dense-body">
          No recurring patterns detected — all remediation jobs appear unique.
        </p>
      </OpsSection>
    )
  }
  return (
    <OpsSection title="Recurring Patterns">
      <div className="overflow-x-auto">
      <DenseDataTable tableClassName="min-w-[800px]">
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead className="w-[28px]" />
            <DenseTableHead className="min-w-[130px]">Pattern</DenseTableHead>
            <DenseTableHead className="w-[75px]">Severity</DenseTableHead>
            <DenseTableHead className="w-[110px]">Root Cause</DenseTableHead>
            <DenseTableHead className="w-[70px]">Confidence</DenseTableHead>
            <DenseTableHead className="w-[50px] text-right">Count</DenseTableHead>
            <DenseTableHead className="w-[60px] text-right">Success</DenseTableHead>
            <DenseTableHead className="w-[40px] text-center">Trend</DenseTableHead>
            <DenseTableHead>Top Tools</DenseTableHead>
            {onFixPattern != null && <DenseTableHead className="w-[72px]" />}
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {patterns.map(p => {
            const isOpen = expanded.has(p.id)
            return (
              <>
                <DenseTableRow key={p.id} className="cursor-pointer" onClick={() => toggle(p.id)}>
                  <DenseTableCell className="w-[28px] px-1">
                    {isOpen
                      ? <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
                      : <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />}
                  </DenseTableCell>
                  <DenseTableCell>
                    <span className="font-medium text-dense-body">{p.label}</span>
                  </DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={severityVariant(p.severity)}>{p.severity}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell>
                    <div className="flex items-center gap-1.5">
                      <div className={`w-2 h-2 rounded-full ${rootCauseColor(p.root_cause)}`} />
                      <span className="text-dense-meta">{rootCauseLabel(p.root_cause)}</span>
                    </div>
                  </DenseTableCell>
                  <DenseTableCell>
                    {confidenceBar(p.confidence)}
                  </DenseTableCell>
                  <DenseTableCell className="text-right font-mono tabular-nums">
                    {p.occurrences}
                  </DenseTableCell>
                  <DenseTableCell className="text-right font-mono tabular-nums">
                    {(p.success_rate ?? 0).toFixed(0)}%
                  </DenseTableCell>
                  <DenseTableCell className="text-center">
                    <TrendIcon trend={p.trending} />
                  </DenseTableCell>
                  <DenseTableCell>
                    <div className="flex gap-1 flex-wrap">
                      {safeActions(p.top_actions).slice(0, 3).map(a => (
                        <span
                          key={a.tool}
                          className="text-dense-caption bg-secondary px-1.5 py-0.5 rounded"
                        >
                          {a.tool}
                          <span className="text-muted-foreground ml-0.5">×{a.count}</span>
                        </span>
                      ))}
                    </div>
                  </DenseTableCell>
                  {onFixPattern != null && (
                    <DenseTableCell>
                      {p.occurrences >= 2 && canFix && (
                        <Button
                          variant="ghost"
                          size="xs"
                          disabled={fixPending}
                          onClick={e => {
                            e.stopPropagation()
                            onFixPattern(p)
                          }}
                        >
                          <Wrench size={12} className="mr-1" aria-hidden />
                          Fix
                        </Button>
                      )}
                    </DenseTableCell>
                  )}
                </DenseTableRow>
                {isOpen && p.signals && p.signals.length > 0 && (
                  <DenseTableRow key={`${p.id}-signals`}>
                    <DenseTableCell colSpan={onFixPattern != null ? 10 : 9} className="!py-2 bg-secondary/30">
                      <div className="pl-6 space-y-1">
                        <p className="text-dense-caption font-medium text-muted-foreground mb-1">
                          Classification signals ({p.signals.length})
                        </p>
                        {p.signals.map((s, i) => (
                          <div key={i} className="flex items-start gap-2 text-dense-caption">
                            <div className={`w-2 h-2 rounded-full mt-1 shrink-0 ${rootCauseColor(s.cause)}`} />
                            <span className="font-mono text-muted-foreground w-8 shrink-0 text-right">
                              {s.weight.toFixed(1)}
                            </span>
                            <span className="text-foreground">{s.name}</span>
                            {s.detail && (
                              <span className="text-muted-foreground">— {s.detail}</span>
                            )}
                          </div>
                        ))}
                      </div>
                    </DenseTableCell>
                  </DenseTableRow>
                )}
              </>
            )
          })}
        </DenseTableBody>
      </DenseDataTable>
      </div>
    </OpsSection>
  )
}

function ScopeStatsTable({ stats }: { stats: RetrospectiveScopeStats[] }) {
  return (
    <OpsSection title="Scope Breakdown">
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Scope</DenseTableHead>
            <DenseTableHead className="text-right w-[60px]">Total</DenseTableHead>
            <DenseTableHead className="text-right w-[60px]">Done</DenseTableHead>
            <DenseTableHead className="text-right w-[60px]">Failed</DenseTableHead>
            <DenseTableHead className="text-right w-[80px]">Success %</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {stats.map(s => (
            <DenseTableRow key={s.scope}>
              <DenseTableCell className="font-medium">{s.scope}</DenseTableCell>
              <DenseTableCell className="text-right font-mono tabular-nums">
                {s.total}
              </DenseTableCell>
              <DenseTableCell className="text-right font-mono tabular-nums text-emerald-400">
                {s.done}
              </DenseTableCell>
              <DenseTableCell className="text-right font-mono tabular-nums text-red-400">
                {s.failed || '—'}
              </DenseTableCell>
              <DenseTableCell className="text-right font-mono tabular-nums">
                {(s.success_rate ?? 0).toFixed(0)}%
              </DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}

function ToolUsageTable({ tools }: { tools: RetrospectiveToolUsage[] }) {
  return (
    <OpsSection title="Tool Usage (top 10)">
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Tool</DenseTableHead>
            <DenseTableHead className="text-right w-[80px]">Calls</DenseTableHead>
            <DenseTableHead className="text-right w-[60px]">Jobs</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {tools.slice(0, 10).map(t => (
            <DenseTableRow key={t.tool}>
              <DenseTableCell>
                <code className="text-dense-meta">{t.tool}</code>
              </DenseTableCell>
              <DenseTableCell className="text-right font-mono tabular-nums">
                {t.count}
              </DenseTableCell>
              <DenseTableCell className="text-right font-mono tabular-nums">
                {t.jobs}
              </DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}

function NamespaceTable({ namespaces }: { namespaces: RetrospectiveNamespaceActivity[] }) {
  return (
    <OpsSection title="Namespace Activity">
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Namespace</DenseTableHead>
            <DenseTableHead className="text-right w-[80px]">Calls</DenseTableHead>
            <DenseTableHead className="text-right w-[60px]">Jobs</DenseTableHead>
            <DenseTableHead>Top Actions</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {namespaces.map(n => (
            <DenseTableRow key={n.namespace}>
              <DenseTableCell>
                <code className="text-dense-meta">{n.namespace}</code>
              </DenseTableCell>
              <DenseTableCell className="text-right font-mono tabular-nums">
                {n.tool_calls}
              </DenseTableCell>
              <DenseTableCell className="text-right font-mono tabular-nums">
                {n.jobs}
              </DenseTableCell>
              <DenseTableCell>
                <div className="flex gap-1 flex-wrap">
                  {safeActions(n.top_actions).slice(0, 3).map(a => (
                    <span
                      key={a.tool}
                      className="text-dense-caption bg-secondary px-1.5 py-0.5 rounded"
                    >
                      {a.tool} ×{a.count}
                    </span>
                  ))}
                </div>
              </DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}

function HighRecurrenceTriageStrip({
  patterns,
  onFixPattern,
  fixPending,
  canFix,
}: {
  patterns: RetrospectivePatternCluster[]
  onFixPattern?: (pattern: RetrospectivePatternCluster) => void
  fixPending?: boolean
  canFix?: boolean
}) {
  const hot = [...patterns]
    .filter(p => p.occurrences >= 2)
    .sort((a, b) => b.occurrences - a.occurrences)
    .slice(0, 5)
  if (hot.length === 0) return null

  function inferTrack(p: RetrospectivePatternCluster): string {
    const label = p.label.toLowerCase()
    if (label.includes('deliver') || label.includes('pipeline') || label.includes('tekton')) return 'playbook'
    if (label.includes('crash') || label.includes('config') || label.includes('nginx')) return 'product'
    if (label.includes('node') || label.includes('postgres') || label.includes('redis')) return 'infra'
    return 'agent-adhoc'
  }

  return (
    <OpsSection
      title="High-recurrence patterns (cluster triage cross-ref)"
      description="Patterns with 2+ agent jobs — see Cluster → Failure triage for live Top-N ranking and Fix actions."
    >
      <ul className="m-0 list-none space-y-1.5 px-3 py-2">
        {hot.map(p => (
          <li key={p.id} className="flex flex-wrap items-center gap-2 text-dense-body">
            <DenseTag variant={p.severity === 'critical' ? 'danger' : 'warning'}>{inferTrack(p)}</DenseTag>
            <span className="font-medium">{p.label}</span>
            <span className="font-mono tabular-nums text-muted-foreground">{p.occurrences}×</span>
            {p.trending === 'up' && <TrendIcon trend="up" />}
            {onFixPattern != null && canFix && (
              <Button
                variant="ghost"
                size="xs"
                disabled={fixPending}
                onClick={() => onFixPattern(p)}
              >
                Fix
              </Button>
            )}
          </li>
        ))}
      </ul>
    </OpsSection>
  )
}

function isReportEmpty(report: RetrospectiveReport): boolean {
  return report.total_jobs === 0 && (report.patterns?.length ?? 0) === 0
}

function safeActions(actions: RetrospectivePatternCluster['top_actions'] | undefined) {
  return actions ?? []
}

export type DefectsPageProps = {
  canOperate?: boolean
  onStartAgentJob?: (job: AmbientAgentJob) => void
}

export function DefectsPage({
  canOperate = false,
  onStartAgentJob,
}: DefectsPageProps = {}) {
  const qc = useQueryClient()
  const patternFixMutation = useMutation({
    mutationFn: (pattern: RetrospectivePatternCluster) =>
      startRemediation({
        scope: DEFECT_PATTERN_REMEDIATE_SCOPE,
        prompt: buildDefectPatternRemediatePrompt(pattern),
      }),
    onSuccess: (job) => {
      void qc.invalidateQueries({ queryKey: ['agent', 'retrospective', 'report'] })
      onStartAgentJob?.({
        id: job.id,
        scope: DEFECT_PATTERN_REMEDIATE_SCOPE,
        label: scopeToLabel(DEFECT_PATTERN_REMEDIATE_SCOPE),
      })
    },
  })

  const handleFixPattern = (pattern: RetrospectivePatternCluster) => {
    if (!canOperate) return
    patternFixMutation.mutate(pattern)
  }

  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ['agent', 'retrospective', 'report'],
    queryFn: () => fetchRetrospectiveReport(),
    refetchInterval: 120_000,
  })

  if (isLoading) {
    return (
      <div className="space-y-4">
        <PageHeader
          title="Defects"
          description="Cross-job pattern analysis — identifying systemic platform issues from remediation history."
        />
        <OpsSection title="Analysis">
          <p className="p-8 text-center text-muted-foreground text-dense-body">
            Analyzing remediation job history…
          </p>
        </OpsSection>
      </div>
    )
  }

  if (error != null) {
    const message = error instanceof Error ? error.message : 'Failed to load retrospective report'
    return (
      <div className="space-y-4">
        <PageHeader
          title="Defects"
          description="Cross-job pattern analysis — identifying systemic platform issues from remediation history."
        />
        <OpsSection title="Analysis">
          <div className="flex flex-col items-center gap-3 p-8 text-center">
            <AlertCircle className="h-8 w-8 text-destructive" />
            <p className="text-dense-body text-destructive">{message}</p>
            <Button variant="outline" size="sm" disabled={isFetching} onClick={() => void refetch()}>
              <RefreshCw className={`mr-1.5 h-3.5 w-3.5 ${isFetching ? 'animate-spin' : ''}`} />
              Retry
            </Button>
          </div>
        </OpsSection>
      </div>
    )
  }

  if (data == null || isReportEmpty(data)) {
    return (
      <div className="space-y-4">
        <PageHeader
          title="Defects"
          description="Cross-job pattern analysis — identifying systemic platform issues from remediation history."
          actions={
            <button
              onClick={() => void refetch()}
              className="inline-flex items-center gap-1.5 text-dense-label text-muted-foreground hover:text-foreground transition-colors"
            >
              <RefreshCw className="h-3.5 w-3.5" />
              Refresh
            </button>
          }
        />
        <OpsSection title="Analysis">
          <p className="p-8 text-center text-muted-foreground text-dense-body">
            No remediation job history yet — patterns will appear after Agent runs complete.
          </p>
        </OpsSection>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title="Defects"
        description="Cross-job pattern analysis — identifying systemic platform issues from remediation history."
        actions={
          <button
            onClick={() => void refetch()}
            className="inline-flex items-center gap-1.5 text-dense-label text-muted-foreground hover:text-foreground transition-colors"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            Refresh
          </button>
        }
      />

      <StatsCards report={data} />
      <HighRecurrenceTriageStrip
        patterns={data.patterns ?? []}
        onFixPattern={handleFixPattern}
        fixPending={patternFixMutation.isPending}
        canFix={canOperate}
      />
      <RootCauseDistBar dist={data.root_cause_distribution ?? []} />
      <InsightsPanel insights={data.insights} />
      <PatternsTable
        patterns={data.patterns ?? []}
        onFixPattern={handleFixPattern}
        fixPending={patternFixMutation.isPending}
        canFix={canOperate}
      />

      <div className="grid grid-cols-2 gap-4">
        <ScopeStatsTable stats={data.scope_stats ?? []} />
        <ToolUsageTable tools={data.tool_usage ?? []} />
      </div>

      <NamespaceTable namespaces={data.namespaces ?? []} />
    </div>
  )
}
