/**
 * Shared helpers / tags for DefectsPage panels.
 * Display mapping only — no retrospective API calls.
 */
import { DenseTag, cn } from '@bifrost/ui'
import { TrendingUp, TrendingDown, Minus } from 'lucide-react'
import {
  SYSTEM_DOMAIN_ICON,
  SYSTEM_DOMAIN_VARIANT,
  systemDomainLabel,
  type SystemDomainId,
} from '@/lib/architecture/systemDomainCatalog'
import type { FleetRole } from '@/lib/control-room/fleetSnapshot'
import {
  FLEET_ROLE_COLOR,
  FLEET_ROLE_ICON,
  FLEET_ROLE_LABEL,
} from '@/lib/control-room/fleetRoleVisuals'
import type {
  RetrospectivePatternCluster,
  RetrospectiveReport,
  RetrospectiveRootCause,
  RetrospectiveSeverity,
} from '@/api/agentTypes'

export function DomainTag({ id }: { id: SystemDomainId }) {
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

export function RoleTag({ role }: { role: FleetRole }) {
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

export function PatternKindTags({
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

export function domainFilterChipClass(selected: boolean): string {
  return cn(selected ? 'ring-1 ring-current/40 brightness-110' : 'opacity-55 hover:opacity-90')
}

export function severityVariant(s: RetrospectiveSeverity) {
  switch (s) {
    case 'critical': return 'danger' as const
    case 'high': return 'warning' as const
    case 'medium': return 'category' as const
    default: return 'category' as const
  }
}

export function rootCauseLabel(r: string) {
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

export function rootCauseColor(r: RetrospectiveRootCause) {
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

export function confidenceBar(confidence: number) {
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

export function TrendIcon({ trend }: { trend: string }) {
  if (trend === 'up') return <TrendingUp className="h-3.5 w-3.5 text-red-400" />
  if (trend === 'down') return <TrendingDown className="h-3.5 w-3.5 text-emerald-400" />
  return <Minus className="h-3.5 w-3.5 text-muted-foreground" />
}

export function isStructuralPattern(p: RetrospectivePatternCluster): boolean {
  return p.root_cause === 'platform_defect' && p.confidence >= 0.6 && p.trending !== 'down'
}

export function isTrendingPattern(p: RetrospectivePatternCluster): boolean {
  return p.trending === 'up'
}

/** Deduped actionable set: trending ∪ structural. */
export function attentionPatterns(patterns: RetrospectivePatternCluster[]): RetrospectivePatternCluster[] {
  const byId = new Map<string, RetrospectivePatternCluster>()
  for (const p of patterns) {
    if (isTrendingPattern(p) || isStructuralPattern(p)) byId.set(p.id, p)
  }
  return [...byId.values()].sort((a, b) => b.occurrences - a.occurrences)
}

/** Remediation-history debt — not Fleet/Launch GO|NO-GO. */
export type PatternDebtLevel = 'CLEAR' | 'ELEVATED' | 'CRITICAL'

export function patternDebtLevel(
  healthScore: number,
  trendingCount: number,
  structuralCount: number,
): PatternDebtLevel {
  if (healthScore >= 90 && trendingCount === 0 && structuralCount === 0) return 'CLEAR'
  if (healthScore < 70 || structuralCount > 0) return 'CRITICAL'
  return 'ELEVATED'
}

export const DEBT_LEVEL_VARIANT: Record<PatternDebtLevel, 'success' | 'warning' | 'danger'> = {
  CLEAR: 'success',
  ELEVATED: 'warning',
  CRITICAL: 'danger',
}

export function isReportEmpty(report: RetrospectiveReport): boolean {
  return report.total_jobs === 0 && (report.patterns?.length ?? 0) === 0
}

export function safeActions(actions: RetrospectivePatternCluster['top_actions'] | undefined) {
  return actions ?? []
}
