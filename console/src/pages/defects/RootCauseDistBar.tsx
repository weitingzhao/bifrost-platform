import { OpsSection } from '@/components/layout/OpsSection'
import type { RetrospectiveRootCauseDistribution } from '@/api/agentTypes'
import { rootCauseColor, rootCauseLabel } from './format'

export function RootCauseDistBar({ dist }: { dist: RetrospectiveRootCauseDistribution[] }) {
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
