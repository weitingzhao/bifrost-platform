import type { StepCell } from './planModuleModel'

/**
 * Why an active step is not done (R5:5.5-19 §6 item 4). In a step's detail: every reason, each with its links, under
 * "Still needed for done" while work on it runs. On the module page (compact): the first reason on a line of its own
 * and how many more the detail has; nothing for a step with work running now (its detail says it). Nothing unread.
 */
export function StepWhy({ cell, compact = false }: { cell: StepCell; compact?: boolean }) {
  const lines = cell.why ?? []
  if (cell.step !== 'active' || lines.length === 0) return null
  const running = cell.now?.kind === 'running'
  if (compact && running) return null
  // Counted by kind, never by lines: the detail's own "+N more" line is no blocker.
  const blockers = lines.filter(l => l.kind === 'blocker').length
  const notListed = lines.find(l => l.kind === 'omitted')?.count ?? 0
  const shown = compact ? lines.filter(l => l.kind !== 'omitted').slice(0, 1) : lines
  const inDetail = compact ? Math.max(0, blockers - shown.filter(l => l.kind === 'blocker').length) : 0
  const text = compact ? 'text-[13px] leading-[1.45]' : ''
  return <div data-step-why={compact ? 'compact' : 'full'} className="flex min-w-0 flex-col gap-0.5">
    {running && <span className="text-xs text-muted-foreground">Still needed for done</span>}
    {shown.map((line, i) => <p key={i} data-step-why-line={line.kind} className={`m-0 text-muted-foreground ${text}`}>
      {line.text}
      {line.links.map(link => <span key={link.href}> <a href={link.href} className="underline-offset-2 hover:underline">{link.text}</a></span>)}
    </p>)}
    {inDetail > 0 && <p data-step-why-more className={`m-0 text-muted-foreground ${text}`}>+{inDetail} more in the step&apos;s detail.</p>}
    {compact && notListed > 0 && <p data-step-why-omitted className={`m-0 text-muted-foreground ${text}`}>{notListed} more not listed (a step lists at most 50 items).</p>}
  </div>
}
