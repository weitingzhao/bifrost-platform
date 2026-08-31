import {
  DenseTag,
  Button,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@bifrost/ui'
import { Wrench } from 'lucide-react'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import { patternToDomain } from '@/lib/architecture/systemDomainCatalog'
import { patternToFleetRole } from '@/lib/architecture/defectPatternFleetRole'
import type { RetrospectivePatternCluster } from '@/api/agentTypes'
import {
  DomainTag,
  RoleTag,
  PatternKindTags,
  severityVariant,
  rootCauseLabel,
  rootCauseColor,
  confidenceBar,
  TrendIcon,
  isStructuralPattern,
  isTrendingPattern,
  safeActions,
} from './format'

export function AttentionPatternSheet({
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
