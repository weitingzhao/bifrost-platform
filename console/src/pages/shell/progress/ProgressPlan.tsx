import { DenseTag } from '@bifrost/ui'
import type { ProgressGroup, ProgressRoadmap } from '@/api/agentProgressBoard'
import type { PlanEstimate } from '@/api/progressFields'
import { SectionFold } from '@/pages/shell/progress/progressFold'
import { planCounts, planMeterLine, planStateLabel } from '@/pages/shell/progress/progressModel'
import {
  agentText,
  formatCount,
  groupNames,
  nightsFrom,
  nightsRange,
  openDetails,
  sliceIsUnknown,
  sliceLabel,
  slicePercent,
  slicesHeadline,
  sumOpen,
  timesHeadline,
  waitsOnOwner,
} from '@/pages/shell/progress/progressSummaryModel'

function Chips({ group }: { group: ProgressGroup }) {
  return (
    <ul className="m-0 flex list-none flex-wrap gap-1 p-0">
      {(group.items ?? []).map((item, index) => (
        <li key={`${item}-${index}`}>
          <DenseTag variant="category" size="pill">
            {item}
          </DenseTag>
        </li>
      ))}
    </ul>
  )
}

function DetailRow({ name, est }: { name: string | undefined; est: PlanEstimate | undefined }) {
  if (est == null || est.unreadable) {
    return (
      <p className="m-0 text-sm">
        <span className="font-medium">{name}</span> <span className="text-muted-foreground">No estimate</span>
      </p>
    )
  }
  const unknown = sliceIsUnknown(est.done, est.total)
  const pct = slicePercent(est.done, est.total)
  const blocked = agentText(est.blocked)
  const note = agentText(est.note)
  const sure = agentText(est.sure)
  const asOf = agentText(est.as_of)
  const owner = waitsOnOwner(blocked)
  const title = agentText(name)
  return (
    <div className="flex min-w-0 flex-col gap-1 border-t border-[var(--card-border)] py-2 text-sm">
      <span className="break-words font-medium">
        {title}
        {owner ? <i data-owner-dot="" className="ml-1 inline-block size-2 rounded-full bg-amber-500" /> : null}
      </span>
      {note !== '' ? <span className="break-words text-muted-foreground">{note}</span> : null}
      <span className="block h-1.5 max-w-full overflow-hidden rounded bg-foreground/10">
        {unknown ? null : <span className="block h-full bg-emerald-500" style={{ width: `${pct ?? 0}%` }} />}
      </span>
      <span className="text-muted-foreground">{unknown ? 'Done Unknown' : `Done ${sliceLabel(est.done, est.total)}`}</span>
      <span>{`Agent slices left ${est.mine === 'unknown' || est.mine == null ? 'Unknown' : formatCount(est.mine)} · times you ${est.owner === 'unknown' || est.owner == null ? 'Unknown' : formatCount(est.owner)}`}</span>
      <span className={owner ? 'text-warning' : ''}>{`Waiting on: ${blocked || '—'}`}</span>
      <span className="text-muted-foreground">{`Confidence: ${sure || '?'}${asOf !== '' ? ` · ${asOf}` : ''}`}</span>
    </div>
  )
}

/** The global plan. History and the arithmetic sit in their own folds. */
export function ProgressPlan({ roadmap }: { roadmap: ProgressRoadmap | undefined }) {
  if (roadmap == null) return null
  const counts = planCounts(roadmap)
  const total = counts.done + counts.doing + counts.todo
  const groups = roadmap.groups ?? []
  const open = openDetails(roadmap)
  const onYou = open.filter(row => !row.detail.unreadable && waitsOnOwner(row.detail.est?.blocked))
  const rest = open.filter(row => row.detail.unreadable || !waitsOnOwner(row.detail.est?.blocked))
  const mine = sumOpen(open, 'mine')
  const ownerTimes = sumOpen(open, 'owner')
  const pace = roadmap.pace
  const nights = pace == null || pace.unreadable ? null : nightsFrom(mine, ownerTimes, pace.per_night)
  const range = mine.kind === 'full' && ownerTimes.kind === 'full' && pace != null && !pace.unreadable ? nightsRange(mine.value, pace.per_night) : null
  const shipped = groupNames(groups, ['done', 'dropped'])
  const from = roadmap.generated_from?.trim() ?? ''
  const basis = pace != null && !pace.unreadable ? agentText(pace.basis) : ''
  const ownerWait = pace != null && !pace.unreadable ? agentText(pace.owner_wait) : ''
  const basisBody = basis !== '' || ownerWait !== '' || roadmap.uncovered_exit?.trim() || roadmap.note?.trim() || from !== ''
  return (
    <section aria-label="Plan" className="flex w-full min-w-0 flex-col gap-2">
      {roadmap.goal?.trim() ? <p className="m-0 break-words text-sm text-foreground">{roadmap.goal}</p> : null}
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
      {nights != null && mine.kind === 'full' && ownerTimes.kind === 'full' ? (
        <p className="m-0 text-sm text-foreground">{`${formatCount(mine.value)} slices left · about ${nights} nights${range != null ? ` (${range})` : ''} · ${formatCount(ownerTimes.value)} times you`}</p>
      ) : null}
      {mine.kind === 'partial' || ownerTimes.kind === 'partial' ? (
        <p className="m-0 text-sm text-foreground">{[slicesHeadline(mine), timesHeadline(ownerTimes)].filter(text => text != null).join(' · ')}</p>
      ) : null}
      {onYou.length > 0 ? (
        <SectionFold id="plan-owner" title="Waiting on you" digest={`${onYou.length}`} defaultOpen>
          {onYou.map((row, index) => (
            <DetailRow key={`${row.detail.name ?? 'you'}-${index}`} name={row.detail.name} est={row.detail.est} />
          ))}
        </SectionFold>
      ) : null}
      <SectionFold id="plan-rest" title="Other open items" digest={`${rest.length || groups.filter(group => group.state === 'doing' || group.state === 'todo').length}`} defaultOpen={false}>
        {rest.map((row, index) => (
          <DetailRow key={`${row.detail.name ?? 'rest'}-${index}`} name={row.detail.name} est={row.detail.est} />
        ))}
        {groups
          .filter(group => group.state === 'doing' || group.state === 'todo')
          .map((group, index) => (
            <div key={`${group.name ?? 'open'}-${index}`} className="flex min-w-0 flex-col gap-1">
              <span className="text-sm text-muted-foreground">{`${group.name ?? ''} · ${planStateLabel(group.state)}`}</span>
              <Chips group={group} />
            </div>
          ))}
      </SectionFold>
      {roadmap.delta != null ? (
        <SectionFold
          id="plan-delta"
          title="Since last time"
          digest={roadmap.delta.unreadable ? 'Unknown' : (roadmap.delta.since ?? '')}
          defaultOpen={false}
        >
          {roadmap.delta.unreadable ? (
            <p className="m-0 text-sm">Could not read this item</p>
          ) : (
            <ul className="m-0 list-disc pl-5 text-sm">
              {roadmap.delta.lines.map((line, index) => {
                const text = agentText(line)
                if (text === '') return null
                return (
                  <li key={index} className="break-words">
                    {text}
                  </li>
                )
              })}
            </ul>
          )}
        </SectionFold>
      ) : null}
      {basisBody ? (
        <SectionFold id="plan-basis" title="How the numbers were computed" defaultOpen={false}>
          {basis !== '' ? <p className="m-0 break-words text-sm text-muted-foreground">{basis}</p> : null}
          {ownerWait !== '' ? <p className="m-0 break-words text-sm text-warning">{ownerWait}</p> : null}
          {roadmap.uncovered_exit?.trim() ? <p className="m-0 break-words text-sm text-muted-foreground">{roadmap.uncovered_exit}</p> : null}
          {roadmap.note?.trim() ? <p className="m-0 break-words text-sm text-muted-foreground">{roadmap.note}</p> : null}
          {from !== '' ? <p className="m-0 break-words text-sm text-muted-foreground">Generated from {from}</p> : null}
        </SectionFold>
      ) : null}
      {shipped !== '' ? (
        <SectionFold
          id="plan-shipped"
          title="Shipped"
          digest={groups
            .filter(group => group.state === 'done' || group.state === 'dropped')
            .map(group => group.name?.trim() ?? '')
            .filter(name => name !== '')
            .map((name, index) => (
              <span key={`${name}-${index}`}>{name}</span>
            ))}
          defaultOpen={false}
        >
          {groups
            .filter(group => group.state === 'done' || group.state === 'dropped')
            .map((group, index) => (
              <div key={`${group.name ?? 'shipped'}-${index}`}>
                <Chips group={group} />
              </div>
            ))}
        </SectionFold>
      ) : null}
      {(roadmap.exit ?? []).length > 0 ? (
        <SectionFold id="plan-exit" title="Exit criteria" digest={`${(roadmap.exit ?? []).filter(row => row.state === 'done').length} / ${(roadmap.exit ?? []).length}`} defaultOpen={false}>
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
        </SectionFold>
      ) : null}
    </section>
  )
}
