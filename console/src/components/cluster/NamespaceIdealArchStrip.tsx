import { DenseTag, Tooltip, TooltipContent, TooltipTrigger } from '@bifrost/ui'
import type { ClusterPlacementRule } from '@/api/types'
import { NodeArchLabel } from '@/components/cluster/NodeArchLabel'
import { StatusLamp } from '@/components/StatusLamp'
import { getNamespacePlacementSummary } from '@/lib/cluster/namespacePlacement'

function IdealArchChips({ archs }: { archs: string[] }) {
  return (
    <span className="inline-flex flex-wrap items-center gap-1">
      {archs.map(arch =>
        arch === 'gpu' ? (
          <DenseTag key={arch} variant="category">
            gpu
          </DenseTag>
        ) : (
          <NodeArchLabel key={arch} arch={arch} showTooltip={false} />
        ),
      )}
    </span>
  )
}

export function NamespaceIdealArchStrip({
  namespace,
  liveRule,
  compact = false,
}: {
  namespace: string
  liveRule?: ClusterPlacementRule
  /** Smaller copy for namespace chips */
  compact?: boolean
}) {
  const summary = getNamespacePlacementSummary(namespace)
  if (!summary.mapped) {
    return compact ? null : (
      <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">Ideal arch: unmapped</span>
    )
  }

  const tooltipBody = (
    <div className="flex max-w-xs flex-col gap-1 text-left">
      {summary.rules.map(rule => (
        <p key={rule.workloadClass} className="m-0 text-[var(--text-dense-meta)]">
          <span className="font-mono-tabular">{rule.workloadClass}</span>
          {' · '}
          {rule.requiredSelector}
          {rule.plannedBinding != null && rule.plannedBinding !== '' ? (
            <>
              <br />
              <span className="text-[var(--muted-foreground)]">→ {rule.plannedBinding}</span>
            </>
          ) : null}
        </p>
      ))}
    </div>
  )

  const content = (
    <span
      className={
        compact
          ? 'cluster-ns-chip__ideal inline-flex items-center gap-1'
          : 'inline-flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)]'
      }
    >
      {!compact && <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">Ideal</span>}
      <IdealArchChips archs={summary.idealArchs} />
      {!compact && liveRule != null && (
        <>
          <StatusLamp value={liveRule.reachability} kind="reach" />
          {!liveRule.satisfied && liveRule.gap_reason != null && liveRule.gap_reason !== '' ? (
            <span className="text-[var(--destructive)]">{liveRule.gap_reason}</span>
          ) : liveRule.satisfied ? (
            <span className="text-[var(--muted-foreground)]">pool OK</span>
          ) : null}
        </>
      )}
    </span>
  )

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex">{content}</span>
      </TooltipTrigger>
      <TooltipContent side="bottom">{tooltipBody}</TooltipContent>
    </Tooltip>
  )
}
