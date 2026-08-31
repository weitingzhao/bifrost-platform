/**
 * Defects / retrospective patterns — live operational page (not a static Governance catalog).
 * DenseTagButton filter chips are intentional for multi-select live data filtering;
 * do not migrate to GovernanceCatalogShell (exclusive tabs suit static catalogs only).
 *
 * Panels live under `./defects/` — this file is fetch + compose only.
 */
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { DenseTagButton, Button, cn } from '@bifrost/ui'
import { AlertCircle, RefreshCw } from 'lucide-react'
import { useMemo, useState } from 'react'
import { fetchRetrospectiveDefects, fetchRetrospectiveReport } from '@/api/agentOps'
import { startRemediation } from '@/api/remediation'
import { buildDefectPatternRemediatePrompt } from '@/lib/agent/defectPatternRemediatePrompt'
import { DEFECT_PATTERN_REMEDIATE_SCOPE } from '@/lib/agent/agentScopes'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import type { AmbientAgentJob } from '@/lib/agent/ambientAgent'
import { OpsSection } from '@/components/layout/OpsSection'
import { PageToolbar } from '@/components/layout/PageToolbar'
import {
  SYSTEM_DOMAINS,
  SYSTEM_DOMAIN_ICON,
  SYSTEM_DOMAIN_VARIANT,
  patternToDomain,
  scopeToDomain,
  type SystemDomainId,
} from '@/lib/architecture/systemDomainCatalog'
import type { RetrospectivePatternCluster } from '@/api/agentTypes'
import { AttentionPanel } from './defects/AttentionPanel'
import { CodeAttributionPanel } from './defects/CodeAttributionPanel'
import { NamespaceTable } from './defects/NamespaceTable'
import { PatternDebtStrip } from './defects/PatternDebtStrip'
import { PatternsTable } from './defects/PatternsTable'
import { RootCauseDistBar } from './defects/RootCauseDistBar'
import { ScopeStatsTable } from './defects/ScopeStatsTable'
import { ToolUsageTable } from './defects/ToolUsageTable'
import {
  domainFilterChipClass,
  isReportEmpty,
  isStructuralPattern,
  isTrendingPattern,
  patternDebtLevel,
} from './defects/format'

export type DefectsPageProps = {
  canOperate?: boolean
  onStartAgentJob?: (job: AmbientAgentJob) => void
}

export function DefectsPage({
  canOperate = false,
  onStartAgentJob,
}: DefectsPageProps = {}) {
  const qc = useQueryClient()
  const [domainFilter, setDomainFilter] = useState<SystemDomainId | 'all'>('all')
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

  const defectsQuery = useQuery({
    queryKey: ['agent', 'retrospective', 'defects'],
    queryFn: () => fetchRetrospectiveDefects(),
    refetchInterval: 120_000,
  })

  const filteredPatterns = useMemo(() => {
    const all = data?.patterns ?? []
    if (domainFilter === 'all') return all
    return all.filter(p => patternToDomain(p) === domainFilter)
  }, [data?.patterns, domainFilter])

  const defectReports = useMemo(() => {
    const fromEndpoint = defectsQuery.data?.defects
    if (fromEndpoint != null) return fromEndpoint
    return data?.defects ?? []
  }, [defectsQuery.data?.defects, data?.defects])

  const filteredScopeStats = useMemo(() => {
    const all = data?.scope_stats ?? []
    if (domainFilter === 'all') return all
    return all.filter(s => scopeToDomain(s.scope) === domainFilter)
  }, [data?.scope_stats, domainFilter])

  const domainFilterBar = (
    <div
      className="flex flex-wrap items-center gap-1.5"
      role="group"
      aria-label="System domain filter"
    >
      <span className="mr-1 shrink-0 text-[var(--text-dense-meta)] font-medium text-muted-foreground">
        Domain:
      </span>
      <DenseTagButton
        size="pill"
        variant="neutral"
        aria-pressed={domainFilter === 'all'}
        className={domainFilterChipClass(domainFilter === 'all')}
        onClick={() => setDomainFilter('all')}
      >
        All
      </DenseTagButton>
      {SYSTEM_DOMAINS.map(d => {
        const Icon = SYSTEM_DOMAIN_ICON[d.id]
        return (
          <DenseTagButton
            key={d.id}
            size="pill"
            variant={SYSTEM_DOMAIN_VARIANT[d.id]}
            aria-pressed={domainFilter === d.id}
            className={cn(domainFilterChipClass(domainFilter === d.id), 'inline-flex items-center gap-1')}
            onClick={() => setDomainFilter(d.id)}
          >
            <Icon className="h-3 w-3 shrink-0" aria-hidden />
            {d.label}
          </DenseTagButton>
        )
      })}
    </div>
  )

  const refreshAction = (
    <button
      type="button"
      onClick={() => void refetch()}
      className="inline-flex items-center gap-1.5 text-dense-label text-muted-foreground hover:text-foreground transition-colors"
    >
      <RefreshCw className={`h-3.5 w-3.5 ${isFetching ? 'animate-spin' : ''}`} />
      Refresh
    </button>
  )

  if (isLoading) {
    return (
      <div className="space-y-4">
        <PageToolbar>{refreshAction}</PageToolbar>
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
        <PageToolbar>{refreshAction}</PageToolbar>
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
        <PageToolbar>{refreshAction}</PageToolbar>
        <OpsSection title="Analysis">
          <p className="p-8 text-center text-muted-foreground text-dense-body">
            No remediation job history yet — patterns will appear after Agent runs complete.
          </p>
        </OpsSection>
      </div>
    )
  }

  const debtClear =
    patternDebtLevel(
      data.health_score ?? 0,
      filteredPatterns.filter(isTrendingPattern).length,
      filteredPatterns.filter(isStructuralPattern).length,
    ) === 'CLEAR'

  return (
    <div className="space-y-4">
      <PatternDebtStrip
        report={data}
        patterns={filteredPatterns}
        onFixTopPattern={handleFixPattern}
        fixPending={patternFixMutation.isPending}
        canFix={canOperate}
      />

      <PageToolbar>{refreshAction}</PageToolbar>

      {domainFilterBar}

      <AttentionPanel
        patterns={filteredPatterns}
        onFixPattern={handleFixPattern}
        fixPending={patternFixMutation.isPending}
        canFix={canOperate}
      />

      <div className="space-y-3" role="region" aria-label="Evidence">
        <RootCauseDistBar dist={data.root_cause_distribution ?? []} />
        <CodeAttributionPanel defects={defectReports} />
        <PatternsTable
          patterns={filteredPatterns}
          onFixPattern={handleFixPattern}
          fixPending={patternFixMutation.isPending}
          canFix={canOperate}
          defaultCollapsed={debtClear}
        />
      </div>

      <details className="panel-elevated overflow-hidden rounded-md">
        <summary className="flex cursor-pointer list-none flex-wrap items-center gap-2 px-3 py-2.5 [&::-webkit-details-marker]:hidden">
          <span className="ops-section-title">Details</span>
          <span className="text-dense-caption text-muted-foreground">
            Scope · Tool · Namespace
          </span>
        </summary>
        <div className="space-y-4 border-t border-border px-3 py-3">
          <div className="grid grid-cols-2 gap-4">
            <ScopeStatsTable stats={filteredScopeStats} />
            <ToolUsageTable tools={data.tool_usage ?? []} />
          </div>
          <NamespaceTable namespaces={data.namespaces ?? []} />
        </div>
      </details>
    </div>
  )
}
