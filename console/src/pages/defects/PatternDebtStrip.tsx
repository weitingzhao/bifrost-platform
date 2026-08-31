import { DenseTag, StatusLamp, Button, cn } from '@bifrost/ui'
import type { RetrospectiveReport, RetrospectivePatternCluster } from '@/api/agentTypes'
import {
  attentionPatterns,
  DEBT_LEVEL_VARIANT,
  isStructuralPattern,
  isTrendingPattern,
  patternDebtLevel,
} from './format'

export function PatternDebtStrip({
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
