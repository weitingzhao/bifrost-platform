import { SIGNOFF_SECTION_HREF } from '@/pages/shell/progress/progressPart'
import { OwnerStop } from './OwnerStop'
import { SignoffWaitingLink } from '@/pages/shell/agents/SignoffWaiting'
import { useState } from 'react'
import { NEEDS_YOU_HREF } from '@/lib/shell/consoleRoutes'
import { needsYouCountText } from '@/pages/shell/needs-you/needsYouModel'
import { ActionLink, Card, Dot } from './OwnerParts'
import { doneSummary } from './doneForYou'
import { plural, waitingRow, type OwnerRow } from './ownerModel'
import type { OwnerConsoleData } from './useOwnerConsole'

/** For your information and Done for you show this many first; the rest open below. */
const WORTH_SHOWN = 6

function DoneRow({ row }: { row: OwnerRow & { undo: string; check: string } }) {
  return <li className="ops-owner-row flex flex-wrap items-start gap-3 rounded-[var(--control-radius)] bg-[var(--control-fill)] p-3">
    <span className="mt-1.5"><Dot tone={row.tone} /></span>
    <div className="flex min-w-0 flex-1 basis-48 flex-col gap-1">
      <span className="break-words font-semibold">{row.title}</span>
      <span className="break-words text-sm text-muted-foreground">{row.detail}</span>
      <span className="break-words text-sm">{row.undo}</span>
      <span className="break-words text-xs text-muted-foreground">{row.check}</span>
    </div>
  </li>
}

function Row({ row }: { row: OwnerRow }) {
  return <li className="ops-owner-row flex flex-wrap items-start gap-3 rounded-[var(--control-radius)] bg-[var(--control-fill)] p-3">
    <span className="mt-1.5"><Dot tone={row.tone} /></span>
    <div className="flex min-w-0 flex-1 basis-48 flex-col gap-1">
      <span className="break-words font-semibold">{row.title}</span>
      <span className="break-words text-sm text-muted-foreground">{row.detail}</span>
      {row.whose && <span className="text-xs font-semibold text-[var(--color-warning)]">{row.whose}</span>}
    </div>
    {row.href && row.action && <ActionLink href={row.href} primary>{row.action}</ActionLink>}
  </li>
}

/**
 * For you, in kinds that are not mixed (the Owner, 2026-11-19): what he approves in the system
 * (requests, questions, agent cards), what only his own hands can do outside it (log in to TWS on the
 * Windows machine), what is only for his information, and what was done for him without a card
 * (law 85: recorded with how to undo it). The first two are his and counted.
 */
export function OwnerYou({ data }: { data: OwnerConsoleData }) {
  const { needsYou, alerts } = data
  const approve: OwnerRow[] = needsYou.decisionItems.map(waitingRow)
  const hands: OwnerRow[] = alerts?.yours ?? []
  // One count for the verdict and the tab: what he approves and what needs his hands.
  const count = data.yours
  const unknown = count.state === 'unknown' ? count.reason : null
  const recent = needsYou.recentCount.state === 'known' ? String(needsYou.recentCount.count) : needsYouCountText(needsYou.recentCount)
  return <section aria-label="For you" className="flex flex-col gap-3">
    <h2 className="sr-only">For you</h2>
    <div className="ops-you-primary">
      {unknown && <p role="alert" className="m-0 text-sm">Not everything could be read, so these lists may be short: {unknown}</p>}
      <Card label="To approve" title={`To approve · ${needsYou.count.state === 'known' ? approve.length : needsYou.count.state === 'unknown' ? 'Unknown' : '…'}`} accent={approve.length > 0} more={{ href: NEEDS_YOU_HREF, text: 'All waiting' }}>
        {approve.length > 0
          ? <ul className="m-0 flex list-none flex-col gap-2 p-0">{approve.map(row => <Row key={row.id} row={row} />)}</ul>
          : needsYou.count.state === 'known' && <p className="m-0 text-sm">Nothing to approve in the system right now.</p>}
        <div className="flex flex-wrap justify-between gap-2 border-t border-[var(--table-rule)] pt-2 text-sm text-muted-foreground">
          <span>{needsYou.count.state !== 'known' ? 'Approvals, questions and agent cards: not all read'
            : needsYou.decide.length === 0 && needsYou.openQuestions.length === 0 && needsYou.runtimeItems.length === 0
            ? 'No approvals, questions or agent cards waiting'
            : [needsYou.decide.length && plural(needsYou.decide.length, 'approval'), needsYou.openQuestions.length && plural(needsYou.openQuestions.length, 'question'),
              needsYou.runtimeItems.length && plural(needsYou.runtimeItems.length, 'agent card')].filter(Boolean).join(' · ') + ' waiting'}</span>
          <span>Handled recently: {recent} · <a href="#records">Records</a></span>
        </div>
      </Card>
      <Card label="Needs your hands" title={`Needs your hands · ${alerts ? hands.length : data.alertsError ? 'Unknown' : '…'}`} accent={hands.length > 0}>
        <p className="m-0 text-sm text-muted-foreground">Only you can do these, outside the system.</p>
        {hands.length > 0
          ? <ul className="m-0 flex list-none flex-col gap-2 p-0">{hands.map(row => <Row key={row.id} row={row} />)}</ul>
          : alerts && <p className="m-0 text-sm">Nothing needs your hands right now.</p>}
      </Card>
      <Card label="Plan sign-offs" title="Plan sign-offs" more={{ href: SIGNOFF_SECTION_HREF, text: 'Review steps' }}>
        <p className="m-0 text-sm text-muted-foreground">Tracked in Work; separate from Needs you.</p>
        <SignoffWaitingLink />
      </Card>
      <OwnerStop controls={data.controls ?? []} readable={data.controlsReadable === true} />
    </div>
  </section>
}

/**
 * What is only to know about, and what was done for him (Owner 2026-11-24: "for your info 优先级不高，排到下面"): its
 * own block, drawn below the laws and the audit on Command.
 */
export function OwnerForYourInfo({ data }: { data: OwnerConsoleData }) {
  const [showAll, setShowAll] = useState(false)
  const [showAllDone, setShowAllDone] = useState(false)
  const done = data.done
  const { needsYou, alerts } = data
  const silent = needsYou.silentItems.length
  const worth: OwnerRow[] = [...(alerts?.worth ?? []), ...(silent > 0 ? [{
    id: 'silent-threads', title: `${plural(silent, 'Claude thread')} went quiet mid-turn`, detail: 'Claude sessions, not system work',
    tone: 'muted' as const, whose: 'The coordinator checks them', href: NEEDS_YOU_HREF, action: 'See them',
  }] : [])]
  return <div className="ops-you-side">
      <Card label="For your information" title={`For your information · ${alerts ? worth.length : data.alertsError ? 'Unknown' : '…'}`} more={{ href: '#status', text: 'Status' }}>
        <p className="m-0 text-sm text-muted-foreground">To know about; not yours to fix.</p>
        {data.alertsError && <p role="alert" className="m-0 text-sm">Alerts could not be read: {data.alertsError}</p>}
        {worth.length > 0
          ? <ul className="m-0 flex list-none flex-col gap-2 p-0">{(showAll ? worth : worth.slice(0, WORTH_SHOWN)).map(row => <Row key={row.id} row={row} />)}</ul>
          : alerts && <p className="m-0 text-sm">Nothing else is firing.</p>}
        {worth.length > WORTH_SHOWN && <button type="button" onClick={() => setShowAll(v => !v)} className="min-h-11 rounded-[var(--control-radius)] border border-[var(--card-border)] text-sm font-semibold">
          {showAll ? 'Show fewer' : `Show ${worth.length - WORTH_SHOWN} more`}</button>}
        {data.standbyEvidence === 'unread' && <p className="m-0 text-xs text-muted-foreground">A node is on planned standby, but the cluster could not show which alerts are its; they are listed here.</p>}
        {alerts && (alerts.noise > 0 || alerts.merged > 0) && <p className="m-0 text-xs text-muted-foreground">
          Not listed: {plural(alerts.noise, 'alert')} from planned standby, info and the watchdog{alerts.merged > 0 ? `; ${alerts.merged} told with the row above it` : ''}.</p>}
      </Card>
      <Card label="Done for you" title={`Done for you · ${done ? `${done.more ? '≥' : ''}${done.done}` : data.doneError ? 'Unknown' : '…'}`} more={{ href: '#records', text: 'Records' }}>
        <p className="m-0 text-sm text-muted-foreground">Done without a card, each with how to undo it. Nothing here waits on you.</p>
        {data.doneError && <p role="alert" className="m-0 text-sm">The action record could not be read: {data.doneError}</p>}
        {done && <p className="m-0 text-sm">{doneSummary(done)}</p>}
        {done && done.rows.length > 0 && <ul className="m-0 flex list-none flex-col gap-2 p-0">
          {(showAllDone ? done.rows : done.rows.slice(0, WORTH_SHOWN)).map(row => <DoneRow key={row.id} row={row} />)}</ul>}
        {done && done.rows.length > WORTH_SHOWN && <button type="button" onClick={() => setShowAllDone(v => !v)} className="min-h-11 rounded-[var(--control-radius)] border border-[var(--card-border)] text-sm font-semibold">
          {showAllDone ? 'Show fewer' : `Show ${done.rows.length - WORTH_SHOWN} more`}</button>}
      </Card>
  </div>
}
