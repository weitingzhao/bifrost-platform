import { DenseTag } from '@bifrost/ui'
import type { ProgressRoadmap } from '@/api/agentProgressBoard'
import { planCounts, planMeterLine, planStateLabel } from '@/pages/shell/progress/progressModel'

/** The global plan: where we are, what is shipped, and what the Owner signs. */
export function ProgressPlan({ roadmap }: { roadmap: ProgressRoadmap | undefined }) {
  if (roadmap == null) return null
  const counts = planCounts(roadmap)
  const total = counts.done + counts.doing + counts.todo
  const from = roadmap.generated_from?.trim() ?? ''
  return (
    <section aria-label="Plan" className="flex w-full min-w-0 flex-col gap-2">
      <h3 className="m-0 text-sm font-medium text-foreground">Plan</h3>
      {roadmap.goal?.trim() ? <p className="m-0 break-words text-sm text-foreground">{roadmap.goal}</p> : null}
      {roadmap.note?.trim() ? <p className="m-0 break-words text-sm text-muted-foreground">{roadmap.note}</p> : null}
      {(roadmap.stages ?? []).length > 0 ? (
        <ol aria-label="Stages" className="m-0 flex w-full min-w-0 list-none flex-wrap gap-2 p-0">
          {(roadmap.stages ?? []).map((stage, index) => (
            <li
              key={`${stage.name ?? 'stage'}-${index}`}
              aria-current={stage.state === 'current' ? 'step' : undefined}
              className="min-w-0 max-w-full break-words text-sm"
            >
              <span className="font-medium text-foreground">{stage.name}</span>
              {stage.what?.trim() ? <span className="block text-muted-foreground">{stage.what}</span> : null}
              {stage.state === 'current' ? <span className="block text-foreground">you are here</span> : null}
            </li>
          ))}
        </ol>
      ) : null}
      <div
        role="meter"
        aria-label="Plan meter"
        aria-valuemin={0}
        aria-valuemax={total > 0 ? total : 1}
        aria-valuenow={counts.done}
        className="flex h-2 w-full min-w-0 overflow-hidden rounded bg-foreground/10"
      >
        {total > 0 ? (
          <>
            <span className="h-full bg-foreground/80" style={{ width: `${(counts.done / total) * 100}%` }} />
            <span className="h-full bg-foreground/45" style={{ width: `${(counts.doing / total) * 100}%` }} />
            <span className="h-full bg-foreground/20" style={{ width: `${(counts.todo / total) * 100}%` }} />
          </>
        ) : null}
      </div>
      <p className="m-0 text-sm text-foreground">{planMeterLine(roadmap)}</p>
      {(roadmap.groups ?? []).length > 0 ? (
        <ul aria-label="Plan groups" className="m-0 flex w-full min-w-0 list-none flex-wrap gap-2 p-0">
          {(roadmap.groups ?? []).map((group, index) => (
            <li key={`${group.name ?? 'group'}-${index}`} className="flex min-w-0 items-center gap-1">
              <DenseTag variant="category" size="pill">
                {group.name}
              </DenseTag>
              <span className="text-xs text-muted-foreground">{planStateLabel(group.state)}</span>
            </li>
          ))}
        </ul>
      ) : null}
      {(roadmap.exit ?? []).length > 0 ? (
        <ul aria-label="Exit criteria" className="m-0 flex w-full min-w-0 list-none flex-col gap-1 p-0">
          {(roadmap.exit ?? []).map((row, index) => (
            <li key={`${row.text ?? 'exit'}-${index}`} className="flex min-w-0 flex-wrap items-center gap-1 text-sm">
              <span className="break-words">{row.text}</span>
              {planStateLabel(row.state) !== '' ? (
                <DenseTag variant={row.state === 'done' ? 'success' : 'neutral'} size="pill">
                  {planStateLabel(row.state)}
                </DenseTag>
              ) : null}
              {row.owner === true ? (
                <DenseTag variant="warning" size="pill">
                  You sign
                </DenseTag>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
      {from !== '' ? <p className="m-0 break-words text-sm text-muted-foreground">Generated from {from}</p> : null}
    </section>
  )
}
