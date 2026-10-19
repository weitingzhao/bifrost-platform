import { OwnerStop } from './OwnerStop'
import { useState } from 'react'
import { needsYouCountText } from '@/pages/shell/needs-you/needsYouModel'
import { ActionLink, Card, Dot } from './OwnerParts'
import { plural, waitingRow, type OwnerRow } from './ownerModel'
import type { OwnerConsoleData } from './useOwnerConsole'

/** Worth a look shows this many first; the rest open below it. */
const WORTH_SHOWN = 6

function Row({ row, onGoal }: { row: OwnerRow; onGoal?: (draft: string) => void }) {
  return <li className="flex flex-wrap items-start gap-3 rounded-[var(--control-radius)] bg-[var(--control-fill)] p-3">
    <span className="mt-1.5"><Dot tone={row.tone} /></span>
    <div className="flex min-w-0 flex-1 basis-48 flex-col gap-1">
      <span className="break-words font-semibold">{row.title}</span>
      <span className="break-words text-sm text-muted-foreground">{row.detail}</span>
      {row.whose && <span className="text-xs font-semibold text-[var(--color-warning)]">{row.whose}</span>}
    </div>
    {row.href && row.action && <ActionLink href={row.href} primary>{row.action}</ActionLink>}
    {row.goalDraft && onGoal && <button type="button" onClick={() => onGoal(row.goalDraft!)}
      className="min-h-11 shrink-0 rounded-[var(--control-radius)] border border-[var(--color-info)] px-3 text-sm font-semibold text-[var(--color-info)]">Make it a goal</button>}
  </li>
}

/** For you: what only the Owner can do, then what is worth a look and whose it is. */
export function OwnerYou({ data, onGoal }: { data: OwnerConsoleData; onGoal: (draft: string) => void }) {
  const [showAll, setShowAll] = useState(false)
  const { needsYou, alerts } = data
  const yours: OwnerRow[] = [...(alerts?.yours ?? []), ...needsYou.decisionItems.map(waitingRow)]
  const silent = needsYou.silentItems.length
  const worth: OwnerRow[] = [...(alerts?.worth ?? []), ...(silent > 0 ? [{
    id: 'silent-threads', title: `${plural(silent, 'Claude thread')} went quiet mid-turn`, detail: 'Claude sessions, not system work',
    tone: 'muted' as const, whose: 'The coordinator checks them', href: '#needs-you', action: 'See them',
  }] : [])]
  // One count for the title, the verdict and the tab: Needs you's decisions and the alerts only he can clear.
  const count = data.yours
  const unknown = count.state === 'unknown' ? count.reason : null
  const title = count.state === 'known' ? String(count.count) : count.state === 'unknown' ? 'Unknown' : '…'
  const recent = needsYou.recentCount.state === 'known' ? String(needsYou.recentCount.count) : needsYouCountText(needsYou.recentCount)
  return <section aria-label="For you" className="flex flex-col gap-3">
    <h2 className="m-0 text-lg font-semibold">For you</h2>
    <div className="grid gap-4 @3xl:grid-cols-2">
      <OwnerStop controls={data.controls ?? []} readable={data.controlsReadable === true} />
      <Card label="Yours" title={`Yours · ${title}`} accent={yours.length > 0 || count.state === 'unknown'} more={{ href: '#needs-you', text: 'Needs you' }}>
        {unknown && <p role="alert" className="m-0 text-sm">Not everything could be read, so this list may be short: {unknown}</p>}
        {yours.length > 0
          ? <ul className="m-0 flex list-none flex-col gap-2 p-0">{yours.map(row => <Row key={row.id} row={row} />)}</ul>
          : count.state === 'known' && <p className="m-0 text-sm">Nothing is yours right now.</p>}
        <div className="flex flex-wrap justify-between gap-2 border-t border-[var(--table-rule)] pt-2 text-sm text-muted-foreground">
          <span>{needsYou.count.state !== 'known' ? 'Approvals, questions and agent cards: not all read'
            : needsYou.decide.length === 0 && needsYou.openQuestions.length === 0 && needsYou.runtimeItems.length === 0
            ? 'No approvals, questions or agent cards waiting'
            : [needsYou.decide.length && plural(needsYou.decide.length, 'approval'), needsYou.openQuestions.length && plural(needsYou.openQuestions.length, 'question'),
              needsYou.runtimeItems.length && plural(needsYou.runtimeItems.length, 'agent card')].filter(Boolean).join(' · ') + ' waiting'}</span>
          <span>Handled recently: {recent} · <a href="#records">Records</a></span>
        </div>
      </Card>
      <Card label="Worth a look" title={`Worth a look · ${alerts ? worth.length : data.alertsError ? 'Unknown' : '…'}`} more={{ href: '#status', text: 'Status' }}>
        <p className="m-0 text-sm text-muted-foreground">Not yours to fix. Hand one to the chief of staff when it should be worked on.</p>
        {data.alertsError && <p role="alert" className="m-0 text-sm">Alerts could not be read: {data.alertsError}</p>}
        {worth.length > 0
          ? <ul className="m-0 flex list-none flex-col gap-2 p-0">{(showAll ? worth : worth.slice(0, WORTH_SHOWN)).map(row => <Row key={row.id} row={row} onGoal={onGoal} />)}</ul>
          : alerts && <p className="m-0 text-sm">Nothing else is firing.</p>}
        {worth.length > WORTH_SHOWN && <button type="button" onClick={() => setShowAll(v => !v)} className="min-h-11 rounded-[var(--control-radius)] border border-[var(--card-border)] text-sm font-semibold">
          {showAll ? 'Show fewer' : `Show ${worth.length - WORTH_SHOWN} more`}</button>}
        {data.standbyEvidence === 'unread' && <p className="m-0 text-xs text-muted-foreground">A node is on planned standby, but the cluster could not show which alerts are its; they are listed here.</p>}
        {alerts && (alerts.noise > 0 || alerts.merged > 0) && <p className="m-0 text-xs text-muted-foreground">
          Not listed: {plural(alerts.noise, 'alert')} from planned standby, info and the watchdog{alerts.merged > 0 ? `; ${alerts.merged} told with the row above it` : ''}.</p>}
      </Card>
    </div>
  </section>
}
