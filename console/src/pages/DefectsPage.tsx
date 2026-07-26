/**
 * Defects / retrospective patterns — live operational page (not a static Governance catalog).
 * DenseTagButton filter chips are intentional for multi-select live data filtering;
 * do not migrate to GovernanceCatalogShell (exclusive tabs suit static catalogs only).
 */
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  DenseDataTable,
  DenseTableHeader,
  DenseTableBody,
  DenseTableHeadRow,
  DenseTableHead,
  DenseTableRow,
  DenseTableCell,
  DenseTableSubheadRow,
  DenseTag,
  DenseTagButton,
  StatusLamp,
  Button,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  cn,
} from '@bifrost/ui'
import { AlertCircle, RefreshCw, TrendingUp, TrendingDown, Minus, ChevronDown, ChevronRight, Wrench } from 'lucide-react'
import { Fragment, useMemo, useState } from 'react'
import { fetchRetrospectiveReport } from '@/api/agentOps'
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
  systemDomainLabel,
  type SystemDomainId,
} from '@/lib/architecture/systemDomainCatalog'
import { patternToFleetRole } from '@/lib/architecture/defectPatternFleetRole'
import type { FleetRole } from '@/lib/control-room/fleetSnapshot'
import {
  FLEET_ROLE_COLOR,
  FLEET_ROLE_ICON,
  FLEET_ROLE_LABEL,
} from '@/lib/control-room/fleetRoleVisuals'
import type { RetrospectiveReport, RetrospectivePatternCluster, RetrospectiveRootCauseDistribution, RetrospectiveScopeStats, RetrospectiveToolUsage, RetrospectiveNamespaceActivity, RetrospectiveSeverity, RetrospectiveRootCause } from '@/api/agentTypes'

function DomainTag({ id }: { id: SystemDomainId }) {
  const Icon = SYSTEM_DOMAIN_ICON[id]
  return (
    <DenseTag
      variant={SYSTEM_DOMAIN_VARIANT[id]}
      className="inline-flex items-center gap-1 font-semibold"
      title={`System Domain: ${systemDomainLabel(id)}`}
    >
      <Icon className="h-3 w-3 shrink-0" aria-hidden />
      {systemDomainLabel(id)}
    </DenseTag>
  )
}

function RoleTag({ role }: { role: FleetRole }) {
  const Icon = FLEET_ROLE_ICON[role]
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 whitespace-nowrap text-dense-body font-semibold',
        FLEET_ROLE_COLOR[role],
      )}
      title={`Fleet Role: ${FLEET_ROLE_LABEL[role]}`}
    >
      <Icon className="h-3.5 w-3.5 shrink-0" aria-hidden />
      <span>{FLEET_ROLE_LABEL[role]}</span>
    </span>
  )
}

function PatternKindTags({
  trending,
  structural,
}: {
  trending: boolean
  structural: boolean
}) {
  return (
    <>
      {structural && <DenseTag variant="danger">structural</DenseTag>}
      {trending && <DenseTag variant="warning">rising</DenseTag>}
    </>
  )
}

function domainFilterChipClass(selected: boolean): string {
  return cn(selected ? 'ring-1 ring-current/40 brightness-110' : 'opacity-55 hover:opacity-90')
}

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

function isStructuralPattern(p: RetrospectivePatternCluster): boolean {
  return p.root_cause === 'platform_defect' && p.confidence >= 0.6 && p.trending !== 'down'
}

function isTrendingPattern(p: RetrospectivePatternCluster): boolean {
  return p.trending === 'up'
}

/** Deduped actionable set: trending ∪ structural. */
function attentionPatterns(patterns: RetrospectivePatternCluster[]): RetrospectivePatternCluster[] {
  const byId = new Map<string, RetrospectivePatternCluster>()
  for (const p of patterns) {
    if (isTrendingPattern(p) || isStructuralPattern(p)) byId.set(p.id, p)
  }
  return [...byId.values()].sort((a, b) => b.occurrences - a.occurrences)
}

/** Remediation-history debt — not Fleet/Launch GO|NO-GO. */
type PatternDebtLevel = 'CLEAR' | 'ELEVATED' | 'CRITICAL'

function patternDebtLevel(
  healthScore: number,
  trendingCount: number,
  structuralCount: number,
): PatternDebtLevel {
  if (healthScore >= 90 && trendingCount === 0 && structuralCount === 0) return 'CLEAR'
  if (healthScore < 70 || structuralCount > 0) return 'CRITICAL'
  return 'ELEVATED'
}

const DEBT_LEVEL_VARIANT: Record<PatternDebtLevel, 'success' | 'warning' | 'danger'> = {
  CLEAR: 'success',
  ELEVATED: 'warning',
  CRITICAL: 'danger',
}

function PatternDebtStrip({
  report,
  patterns,
  onFixTopPattern,
  fixPending,
  canFix,
}: {
  report: RetrospectiveReport
  patterns: RetrospectivePatternCluster[]
  onFixTopPattern?: (pattern: RetrospectivePatternCluster) => void
  fixPending?: boolean
  canFix?: boolean
}) {
  const score = report.health_score ?? 0
  const trendingCount = patterns.filter(isTrendingPattern).length
  const structuralCount = patterns.filter(isStructuralPattern).length
  const attention = attentionPatterns(patterns)
  const attentionCount = attention.length
  const topPattern = attention[0] ?? null
  const level = patternDebtLevel(score, trendingCount, structuralCount)
  const lampValue = level === 'CLEAR' ? 'ok' : level === 'ELEVATED' ? 'degraded' : 'fail'

  let done = 0
  let failed = 0
  for (const ss of report.scope_stats ?? []) {
    done += ss.done
    failed += ss.failed
  }
  const successRate = done + failed > 0 ? Math.round((done / (done + failed)) * 100) : null

  const metrics: Array<{ label: string; value: string; hint?: string; tone?: string }> = [
    {
      label: 'Retro score',
      value: score.toFixed(0),
      hint: 'from job history',
      tone:
        score >= 90
          ? 'text-emerald-400'
          : score >= 70
            ? 'text-amber-400'
            : 'text-red-400',
    },
    {
      label: 'Jobs',
      value: String(report.total_jobs),
      hint: done + failed > 0 ? `${done} done / ${failed} failed` : undefined,
    },
    {
      label: 'Success',
      value: successRate != null ? `${successRate}%` : '—',
      tone:
        successRate == null
          ? undefined
          : successRate >= 90
            ? 'text-emerald-400'
            : successRate >= 70
              ? 'text-amber-400'
              : 'text-red-400',
    },
    {
      label: 'Patterns',
      value: String(patterns.length),
      hint: `${attentionCount} attention`,
    },
    {
      label: 'Trending',
      value: String(trendingCount),
      tone: trendingCount > 0 ? 'text-amber-400' : undefined,
    },
    {
      label: 'Structural',
      value: String(structuralCount),
      tone: structuralCount > 0 ? 'text-red-400' : undefined,
    },
  ]

  return (
    <section
      className="page-section panel-elevated overflow-hidden"
      aria-label="Pattern debt verdict"
    >
      <div className="flex flex-wrap items-center gap-3 border-b border-border px-3 py-2">
        <StatusLamp value={lampValue} />
        <span className="text-[var(--text-dense-label)] font-semibold tracking-wide">
          PATTERN DEBT
        </span>
        <DenseTag variant={DEBT_LEVEL_VARIANT[level]} size="pill">
          {level}
        </DenseTag>
        {report.analysis_window != null && report.analysis_window !== '' && (
          <span className="text-dense-caption text-muted-foreground">{report.analysis_window}</span>
        )}
        <span className="min-w-0 flex-1 font-mono tabular-nums text-dense-caption text-muted-foreground">
          {report.namespaces?.length ?? 0} ns · {report.tool_usage?.length ?? 0} tools · remediation
          history only
        </span>
        {canFix === true && topPattern != null && onFixTopPattern != null && (
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="shrink-0"
            disabled={fixPending === true}
            onClick={() => onFixTopPattern(topPattern)}
          >
            Fix top pattern
          </Button>
        )}
      </div>
      <div className="grid grid-cols-2 divide-x divide-y divide-border sm:grid-cols-3 lg:grid-cols-6 lg:divide-y-0">
        {metrics.map(m => (
          <div key={m.label} className="flex min-w-0 flex-col gap-0.5 px-3 py-2.5">
            <span className="text-dense-meta text-muted-foreground">{m.label}</span>
            <span className={cn('font-mono text-xl font-bold tabular-nums leading-none', m.tone)}>
              {m.value}
            </span>
            {m.hint != null && m.hint !== '' && (
              <span className="truncate text-dense-caption text-muted-foreground">{m.hint}</span>
            )}
          </div>
        ))}
      </div>
    </section>
  )
}

function AttentionPanel({
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
  const [selected, setSelected] = useState<RetrospectivePatternCluster | null>(null)
  const rows = attentionPatterns(patterns)

  if (rows.length === 0) {
    return (
      <OpsSection title="Attention">
        <p className="px-3 py-3 text-dense-body text-muted-foreground">
          Clear — no trending-up or high-confidence platform defects in this Domain filter.
        </p>
      </OpsSection>
    )
  }

  const showFix = onFixPattern != null && canFix === true

  return (
    <>
      <OpsSection
        title="Attention"
        description="Role = Fleet Desk identity (Rocket / Satellite / …). Domain = Apollo sidebar plane that ran the check. Kind tags are history signals — not live health. Click row for detail; Fix dispatches defect-pattern-remediate."
        bodyPadding="none"
      >
        <div className="overflow-x-auto">
          <DenseDataTable tableClassName="min-w-[960px] !table-auto" wrapClassName="border-0 rounded-none">
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead className="!max-w-none min-w-[7.5rem] whitespace-nowrap">
                  Role
                </DenseTableHead>
                <DenseTableHead className="!max-w-none min-w-[10rem] whitespace-nowrap">
                  Domain
                </DenseTableHead>
                <DenseTableHead className="!max-w-none min-w-[9rem] whitespace-nowrap">
                  Kind
                </DenseTableHead>
                <DenseTableHead className="min-w-[16rem]">Pattern</DenseTableHead>
                <DenseTableHead className="!max-w-none w-[4.5rem] text-right whitespace-nowrap">
                  Count
                </DenseTableHead>
                {showFix && (
                  <DenseTableHead className="!max-w-none w-[4.5rem] whitespace-nowrap" />
                )}
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {rows.map(p => {
                const trending = isTrendingPattern(p)
                const structural = isStructuralPattern(p)
                const domain = patternToDomain(p)
                const role = patternToFleetRole(p)
                return (
                  <DenseTableRow
                    key={p.id}
                    className={cn(
                      'cursor-pointer',
                      structural ? 'bg-red-500/[0.04]' : 'bg-amber-500/[0.04]',
                    )}
                    onClick={() => setSelected(p)}
                  >
                    <DenseTableCell className="!max-w-none whitespace-nowrap">
                      <RoleTag role={role} />
                    </DenseTableCell>
                    <DenseTableCell className="!max-w-none whitespace-nowrap">
                      <DomainTag id={domain} />
                    </DenseTableCell>
                    <DenseTableCell className="!max-w-none">
                      <div className="flex flex-wrap gap-1">
                        <PatternKindTags trending={trending} structural={structural} />
                      </div>
                    </DenseTableCell>
                    <DenseTableCell>
                      <span className="font-medium text-dense-body">{p.label}</span>
                    </DenseTableCell>
                    <DenseTableCell className="!max-w-none text-right font-mono tabular-nums text-dense-caption text-muted-foreground whitespace-nowrap">
                      {p.occurrences}×
                    </DenseTableCell>
                    {showFix && (
                      <DenseTableCell className="!max-w-none whitespace-nowrap">
                        {p.occurrences >= 2 && (
                          <Button
                            variant="ghost"
                            size="xs"
                            disabled={fixPending}
                            onClick={e => {
                              e.stopPropagation()
                              onFixPattern?.(p)
                            }}
                          >
                            <Wrench size={12} className="mr-1" aria-hidden />
                            Fix
                          </Button>
                        )}
                      </DenseTableCell>
                    )}
                  </DenseTableRow>
                )
              })}
            </DenseTableBody>
          </DenseDataTable>
        </div>
      </OpsSection>

      <AttentionPatternSheet
        pattern={selected}
        onOpenChange={open => {
          if (!open) setSelected(null)
        }}
        onFixPattern={onFixPattern}
        fixPending={fixPending}
        canFix={canFix}
      />
    </>
  )
}

function AttentionPatternSheet({
  pattern,
  onOpenChange,
  onFixPattern,
  fixPending,
  canFix,
}: {
  pattern: RetrospectivePatternCluster | null
  onOpenChange: (open: boolean) => void
  onFixPattern?: (pattern: RetrospectivePatternCluster) => void
  fixPending?: boolean
  canFix?: boolean
}) {
  const open = pattern != null
  const trending = pattern != null && isTrendingPattern(pattern)
  const structural = pattern != null && isStructuralPattern(pattern)
  const showFix = pattern != null && onFixPattern != null && canFix === true && pattern.occurrences >= 2

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-full max-w-md flex-col gap-0 overflow-y-auto p-0 sm:max-w-lg">
        {pattern != null && (
          <>
            <SheetHeader className="shrink-0 space-y-2 border-b border-border px-4 py-3 pr-12 text-left">
              <div className="flex flex-wrap items-center gap-1.5">
                <RoleTag role={patternToFleetRole(pattern)} />
                <span className="text-border" aria-hidden>
                  ·
                </span>
                <DomainTag id={patternToDomain(pattern)} />
                <PatternKindTags trending={trending} structural={structural} />
                <DenseTag variant={severityVariant(pattern.severity)}>{pattern.severity}</DenseTag>
              </div>
              <SheetTitle className="text-dense-body leading-snug">{pattern.label}</SheetTitle>
              <SheetDescription className="text-dense-caption">
                Role = Fleet Desk subject. Domain = Apollo plane that ran the check — not a live Domain alert.
              </SheetDescription>
            </SheetHeader>

            <div className="flex flex-col gap-4 px-4 py-3">
              {pattern.description != null && pattern.description.trim() !== '' && (
                <p className="m-0 text-dense-body text-muted-foreground">{pattern.description}</p>
              )}

              <dl className="grid grid-cols-2 gap-x-3 gap-y-2.5 text-dense-meta">
                <div>
                  <dt className="text-muted-foreground">Root cause</dt>
                  <dd className="m-0 mt-0.5 flex items-center gap-1.5">
                    <div className={`h-2 w-2 rounded-full ${rootCauseColor(pattern.root_cause)}`} />
                    {rootCauseLabel(pattern.root_cause)}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Confidence</dt>
                  <dd className="m-0 mt-0.5">{confidenceBar(pattern.confidence)}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Occurrences</dt>
                  <dd className="m-0 mt-0.5 font-mono tabular-nums">{pattern.occurrences}×</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Job success</dt>
                  <dd className="m-0 mt-0.5 font-mono tabular-nums">
                    {(pattern.success_rate ?? 0).toFixed(0)}%
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Trend</dt>
                  <dd className="m-0 mt-0.5 flex items-center gap-1.5">
                    <TrendIcon trend={pattern.trending} />
                    <span className="capitalize">{pattern.trending}</span>
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Avg duration</dt>
                  <dd className="m-0 mt-0.5 font-mono tabular-nums">
                    {(pattern.avg_duration_seconds ?? 0) > 0
                      ? `${Math.round(pattern.avg_duration_seconds)}s`
                      : '—'}
                  </dd>
                </div>
                <div className="col-span-2">
                  <dt className="text-muted-foreground">Seen</dt>
                  <dd className="m-0 mt-0.5 font-mono tabular-nums text-dense-caption">
                    {pattern.first_seen || '—'} → {pattern.last_seen || '—'}
                  </dd>
                </div>
              </dl>

              {safeActions(pattern.top_actions).length > 0 && (
                <div>
                  <p className="mb-1.5 text-dense-meta font-medium text-muted-foreground">Top tools</p>
                  <div className="flex flex-wrap gap-1">
                    {safeActions(pattern.top_actions).slice(0, 8).map(a => (
                      <span
                        key={a.tool}
                        className="rounded bg-secondary px-1.5 py-0.5 font-mono text-dense-caption"
                      >
                        {a.tool}
                        <span className="ml-0.5 text-muted-foreground">×{a.count}</span>
                      </span>
                    ))}
                  </div>
                </div>
              )}

              {pattern.signals != null && pattern.signals.length > 0 && (
                <div>
                  <p className="mb-1.5 text-dense-meta font-medium text-muted-foreground">
                    Classification signals ({pattern.signals.length})
                  </p>
                  <ul className="m-0 list-none space-y-1">
                    {pattern.signals.map((s, i) => (
                      <li key={i} className="flex items-start gap-2 text-dense-caption">
                        <div className={`mt-1 h-2 w-2 shrink-0 rounded-full ${rootCauseColor(s.cause)}`} />
                        <span className="w-8 shrink-0 text-right font-mono text-muted-foreground">
                          {s.weight.toFixed(1)}
                        </span>
                        <span>
                          {s.name}
                          {s.detail != null && s.detail !== '' && (
                            <span className="text-muted-foreground"> — {s.detail}</span>
                          )}
                        </span>
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {pattern.jobs != null && pattern.jobs.length > 0 && (
                <div>
                  <p className="mb-1.5 text-dense-meta font-medium text-muted-foreground">
                    Related jobs ({pattern.jobs.length})
                  </p>
                  <ul className="m-0 max-h-40 list-none space-y-1 overflow-y-auto">
                    {pattern.jobs.slice(0, 12).map(j => (
                      <li
                        key={j.id}
                        className="flex flex-wrap items-center gap-2 font-mono text-dense-caption"
                      >
                        <span className="truncate text-muted-foreground" title={j.id}>
                          {j.id.slice(0, 8)}…
                        </span>
                        {j.scope != null && j.scope !== '' && (
                          <span className="truncate">{scopeToLabel(j.scope)}</span>
                        )}
                        {j.status != null && (
                          <span className="text-muted-foreground">{j.status}</span>
                        )}
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {showFix && (
                <div className="border-t border-border pt-3">
                  <Button
                    size="sm"
                    disabled={fixPending}
                    onClick={() => onFixPattern?.(pattern)}
                  >
                    <Wrench size={14} className="mr-1.5" aria-hidden />
                    Fix — dispatch defect-pattern-remediate
                  </Button>
                </div>
              )}
            </div>
          </>
        )}
      </SheetContent>
    </Sheet>
  )
}

function RootCauseDistBar({ dist }: { dist: RetrospectiveRootCauseDistribution[] }) {
  if (!dist || dist.length === 0) return null
  return (
    <OpsSection title="Root cause" description="Cause mix across remediation jobs in the analysis window.">
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

function groupPatternsByDomain(
  patterns: RetrospectivePatternCluster[],
): Array<{ domain: SystemDomainId; items: RetrospectivePatternCluster[] }> {
  const byDomain = new Map<SystemDomainId, RetrospectivePatternCluster[]>()
  for (const p of patterns) {
    const d = patternToDomain(p)
    const list = byDomain.get(d) ?? []
    list.push(p)
    byDomain.set(d, list)
  }
  return SYSTEM_DOMAINS.map(d => ({ domain: d.id, items: byDomain.get(d.id) ?? [] })).filter(
    g => g.items.length > 0,
  )
}

type PatternSortKey =
  | 'label'
  | 'severity'
  | 'root_cause'
  | 'confidence'
  | 'occurrences'
  | 'success_rate'
  | 'trending'

type PatternSortDir = 'asc' | 'desc'

const SEVERITY_RANK: Record<RetrospectiveSeverity, number> = {
  critical: 4,
  high: 3,
  medium: 2,
  low: 1,
}

const TREND_RANK: Record<'up' | 'stable' | 'down', number> = {
  up: 3,
  stable: 2,
  down: 1,
}

function comparePatterns(
  a: RetrospectivePatternCluster,
  b: RetrospectivePatternCluster,
  key: PatternSortKey,
  dir: PatternSortDir,
): number {
  let cmp = 0
  switch (key) {
    case 'label':
      cmp = a.label.localeCompare(b.label)
      break
    case 'severity':
      cmp = SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity]
      break
    case 'root_cause':
      cmp = a.root_cause.localeCompare(b.root_cause)
      break
    case 'confidence':
      cmp = a.confidence - b.confidence
      break
    case 'occurrences':
      cmp = a.occurrences - b.occurrences
      break
    case 'success_rate':
      cmp = (a.success_rate ?? 0) - (b.success_rate ?? 0)
      break
    case 'trending':
      cmp = TREND_RANK[a.trending] - TREND_RANK[b.trending]
      break
  }
  if (cmp === 0) cmp = b.occurrences - a.occurrences
  return dir === 'asc' ? cmp : -cmp
}

const PATTERN_SORT_COLUMNS: Array<{
  key: PatternSortKey
  short: string
  full: string
  align?: 'left' | 'right' | 'center'
  className?: string
}> = [
  { key: 'label', short: 'Pattern', full: 'Pattern', className: '!max-w-none min-w-[14rem]' },
  { key: 'severity', short: 'Sev', full: 'Severity', className: '!max-w-none min-w-[4.5rem] whitespace-nowrap' },
  { key: 'root_cause', short: 'Cause', full: 'Root Cause', className: '!max-w-none min-w-[7rem] whitespace-nowrap' },
  { key: 'confidence', short: 'Conf', full: 'Confidence', className: '!max-w-none min-w-[5rem] whitespace-nowrap' },
  { key: 'occurrences', short: 'Cnt', full: 'Count', align: 'right', className: '!max-w-none min-w-[3.5rem] whitespace-nowrap' },
  { key: 'success_rate', short: 'OK%', full: 'Success rate', align: 'right', className: '!max-w-none min-w-[3.5rem] whitespace-nowrap' },
  { key: 'trending', short: 'Trend', full: 'Trend', align: 'center', className: '!max-w-none min-w-[3.5rem] whitespace-nowrap' },
]

function SortablePatternHead({
  short,
  full,
  sortKey,
  activeKey,
  dir,
  onSort,
  align,
  className,
}: {
  short: string
  full: string
  sortKey: PatternSortKey
  activeKey: PatternSortKey
  dir: PatternSortDir
  onSort: (key: PatternSortKey) => void
  align?: 'left' | 'right' | 'center'
  className?: string
}) {
  const active = activeKey === sortKey
  return (
    <DenseTableHead
      className={cn('cursor-pointer select-none hover:text-foreground', className)}
      align={align}
      title={`${full} — click to sort`}
      aria-label={`${full}, sort`}
      aria-sort={active ? (dir === 'asc' ? 'ascending' : 'descending') : 'none'}
      onClick={() => onSort(sortKey)}
    >
      <span className="inline-flex items-center gap-0.5 whitespace-nowrap">
        <span>{short}</span>
        {active ? (
          <span className="text-foreground" aria-hidden>
            {dir === 'asc' ? '↑' : '↓'}
          </span>
        ) : null}
      </span>
    </DenseTableHead>
  )
}

function PatternsTable({
  patterns,
  onFixPattern,
  fixPending,
  canFix,
  defaultCollapsed = false,
}: {
  patterns: RetrospectivePatternCluster[]
  onFixPattern?: (pattern: RetrospectivePatternCluster) => void
  fixPending?: boolean
  canFix?: boolean
  /** When true (e.g. debt CLEAR), collapse the patterns body by default. */
  defaultCollapsed?: boolean
}) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [sortKey, setSortKey] = useState<PatternSortKey>('occurrences')
  const [sortDir, setSortDir] = useState<PatternSortDir>('desc')

  const toggle = (id: string) =>
    setExpanded(prev => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })

  const handleSort = (key: PatternSortKey) => {
    if (key === sortKey) {
      setSortDir(d => (d === 'asc' ? 'desc' : 'asc'))
      return
    }
    setSortKey(key)
    setSortDir(key === 'label' || key === 'root_cause' ? 'asc' : 'desc')
  }

  const groups = useMemo(() => {
    return groupPatternsByDomain(patterns).map(g => ({
      domain: g.domain,
      items: [...g.items].sort((a, b) => {
        const roleCmp = patternToFleetRole(a).localeCompare(patternToFleetRole(b))
        if (roleCmp !== 0 && sortKey === 'label') return roleCmp
        const primary = comparePatterns(a, b, sortKey, sortDir)
        if (primary !== 0) return primary
        return roleCmp
      }),
    }))
  }, [patterns, sortKey, sortDir])

  const colSpan = onFixPattern != null ? 11 : 10

  if (patterns.length === 0) {
    return (
      <OpsSection
        title="Patterns"
        description="Grouped by Apollo System Domain. Role = Fleet Desk identity for the target — Domain alone is not enough."
        collapsible={defaultCollapsed}
        defaultCollapsed={defaultCollapsed}
      >
        <p className="p-6 text-center text-muted-foreground text-dense-body">
          No recurring patterns match this filter.
        </p>
      </OpsSection>
    )
  }
  return (
    <OpsSection
      title="Patterns"
      description="Grouped by Apollo System Domain. Role = Fleet Desk identity (Rocket / Satellite / …) for the target; Domain = plane that ran the check. Hover headers for full names; click to sort within each Domain."
      collapsible={defaultCollapsed}
      defaultCollapsed={defaultCollapsed}
    >
      <div className="overflow-x-auto">
      <DenseDataTable tableClassName="min-w-[1100px] !table-auto">
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead className="!max-w-none w-[28px]" />
            <DenseTableHead className="!max-w-none min-w-[7.5rem] whitespace-nowrap" title="Fleet Desk Role">
              Role
            </DenseTableHead>
            {PATTERN_SORT_COLUMNS.map(col => (
              <SortablePatternHead
                key={col.key}
                short={col.short}
                full={col.full}
                sortKey={col.key}
                activeKey={sortKey}
                dir={sortDir}
                onSort={handleSort}
                align={col.align}
                className={col.className}
              />
            ))}
            <DenseTableHead className="!max-w-none min-w-[10rem]" title="Top tools used in this pattern">
              Top Tools
            </DenseTableHead>
            {onFixPattern != null && (
              <DenseTableHead className="!max-w-none w-[4.5rem] whitespace-nowrap" />
            )}
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {groups.map(group => (
            <Fragment key={group.domain}>
              <DenseTableSubheadRow>
                <DenseTableCell colSpan={colSpan} className="!max-w-none">
                  <div className="flex flex-wrap items-center gap-2">
                    <DomainTag id={group.domain} />
                    <span className="font-mono tabular-nums text-dense-caption text-muted-foreground">
                      {group.items.length} pattern{group.items.length === 1 ? '' : 's'}
                    </span>
                    <span className="font-mono tabular-nums text-dense-caption text-muted-foreground">
                      · {group.items.reduce((n, p) => n + p.occurrences, 0)}× jobs
                    </span>
                  </div>
                </DenseTableCell>
              </DenseTableSubheadRow>
              {group.items.map(p => {
                const isOpen = expanded.has(p.id)
                const role = patternToFleetRole(p)
                return (
                  <Fragment key={p.id}>
                    <DenseTableRow className="cursor-pointer" onClick={() => toggle(p.id)}>
                      <DenseTableCell className="!max-w-none w-[28px] px-1">
                        {isOpen
                          ? <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
                          : <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />}
                      </DenseTableCell>
                      <DenseTableCell className="!max-w-none whitespace-nowrap">
                        <RoleTag role={role} />
                      </DenseTableCell>
                      <DenseTableCell>
                        <span className="font-medium text-dense-body">{p.label}</span>
                      </DenseTableCell>
                      <DenseTableCell className="!max-w-none whitespace-nowrap">
                        <DenseTag variant={severityVariant(p.severity)}>{p.severity}</DenseTag>
                      </DenseTableCell>
                      <DenseTableCell className="!max-w-none whitespace-nowrap">
                        <div className="flex items-center gap-1.5" title={rootCauseLabel(p.root_cause)}>
                          <div className={`w-2 h-2 shrink-0 rounded-full ${rootCauseColor(p.root_cause)}`} />
                          <span className="text-dense-meta">{rootCauseLabel(p.root_cause)}</span>
                        </div>
                      </DenseTableCell>
                      <DenseTableCell className="!max-w-none whitespace-nowrap">
                        {confidenceBar(p.confidence)}
                      </DenseTableCell>
                      <DenseTableCell className="!max-w-none text-right font-mono tabular-nums whitespace-nowrap">
                        {p.occurrences}
                      </DenseTableCell>
                      <DenseTableCell className="!max-w-none text-right font-mono tabular-nums whitespace-nowrap">
                        {(p.success_rate ?? 0).toFixed(0)}%
                      </DenseTableCell>
                      <DenseTableCell className="!max-w-none text-center whitespace-nowrap">
                        <TrendIcon trend={p.trending} />
                      </DenseTableCell>
                      <DenseTableCell className="!max-w-none">
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
                        <DenseTableCell className="!max-w-none whitespace-nowrap">
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
                      <DenseTableRow>
                        <DenseTableCell colSpan={colSpan} className="!max-w-none !py-2 bg-secondary/30">
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
                  </Fragment>
                )
              })}
            </Fragment>
          ))}
        </DenseTableBody>
      </DenseDataTable>
      </div>
    </OpsSection>
  )
}

function ScopeStatsTable({ stats }: { stats: RetrospectiveScopeStats[] }) {
  return (
    <OpsSection title="Scope Breakdown" variant="flat">
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead className="w-[110px]">Domain</DenseTableHead>
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
              <DenseTableCell>
                <DomainTag id={scopeToDomain(s.scope)} />
              </DenseTableCell>
              <DenseTableCell className="font-medium">{scopeToLabel(s.scope)}</DenseTableCell>
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
    <OpsSection title="Tool Usage (top 10)" variant="flat">
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
    <OpsSection title="Namespace Activity" variant="flat">
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

  const filteredPatterns = useMemo(() => {
    const all = data?.patterns ?? []
    if (domainFilter === 'all') return all
    return all.filter(p => patternToDomain(p) === domainFilter)
  }, [data?.patterns, domainFilter])

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
