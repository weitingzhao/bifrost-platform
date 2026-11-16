import { useState, type ReactNode } from 'react'
import type { PlanNode, PlanSnapshot } from '@/api/planSnapshot'
import { acceptanceOf, dependentsOf, liveOn, type ModuleScope } from '@/pages/shell/agents/moduleDetailModel'
import { noViewReason, type ModuleRow } from '@/pages/shell/agents/planModuleModel'
import { validity } from '@/pages/shell/agents/planSnapshotModel'
import { WorkRow } from './PlanLiveWork'

const panel = 'flex min-w-0 flex-col rounded-[20px] border border-[var(--ops-edge)] p-3.5 shadow-[var(--ops-shadow)] [background:var(--ops-panel)] [overflow-wrap:anywhere]'
const caps = 'm-0 text-xs font-semibold uppercase tracking-[.08em] text-muted-foreground'
const note = 'm-0 text-xs leading-[1.45] text-muted-foreground'
const dashedPill = 'inline-flex h-5 shrink-0 items-center whitespace-nowrap rounded-full border border-dashed border-[var(--color-lamp-gray)] px-[7px] text-[11px] font-semibold text-muted-foreground'

function CardHead({ title, children }: { title: string; children?: ReactNode }) {
  return <div className="flex min-w-0 flex-wrap items-center justify-between gap-x-2 gap-y-1"><h4 className={caps}>{title}</h4>{children}</div>
}
export function Acceptance({ view, item }: { view: PlanSnapshot; item: PlanNode }) {
  const [open, setOpen] = useState<Record<string, boolean>>({})
  const acc = acceptanceOf(view, item)
  const counts = acc.state === 'ok' ? [`${acc.verified} verified`, acc.notVerified > 0 && `${acc.notVerified} not verified`, acc.unknown > 0 && `${acc.unknown} Unknown`].filter(Boolean).join(' · ') : ''
  return <section aria-label={`Acceptance of ${item.label}`} className={`${panel} gap-2.5`}>
    <CardHead title={acc.state === 'ok' ? `Acceptance · ${acc.criteria.length}` : 'Acceptance'}>
      {acc.state === 'ok' ? acc.criteria.length > 0 && <span data-acceptance-count className={dashedPill}>{counts}</span> : <span className={dashedPill}>Unknown</span>}
    </CardHead>
    {acc.state === 'unknown' ? <p className="m-0 text-sm">Acceptance Unknown · {acc.reason}</p>
      : acc.criteria.length === 0 ? <p className="m-0 text-sm">No acceptance criteria for {item.label} in the adopted plan view.</p>
      : <ul className="m-0 flex list-none flex-col gap-2 p-0">{acc.criteria.map(c => <li key={c.id} data-criterion={c.id} data-verdict={c.verdict}
        className="flex min-w-0 flex-col gap-1.5 rounded-[14px] px-3 py-2.5 [background:var(--ops-selection)]">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <span className="font-mono text-xs text-muted-foreground">{c.milestone}</span>
          <span className="inline-flex h-5 items-center whitespace-nowrap rounded-md border-[1.5px] border-foreground px-1.5 font-mono text-[10px] font-bold uppercase tracking-[.06em]">{c.environment}</span>
          {c.evidence.length > 0 && <button type="button" aria-expanded={!!open[c.id]} onClick={() => setOpen(o => ({ ...o, [c.id]: !o[c.id] }))}
            className="ml-auto inline-flex min-h-8 items-center whitespace-nowrap border-0 bg-transparent p-0 text-xs text-muted-foreground pointer-coarse:min-h-11">
            {c.evidence.length} evidence required {open[c.id] ? '−' : '+'}</button>}
        </div>
        <span className="text-sm font-semibold leading-[1.45]">{c.criterion}</span>
        {c.readings.length === 0 ? <span className="text-xs text-muted-foreground">Not bound to a trusted source · Unknown</span>
          : <ul className="m-0 flex list-none flex-col gap-0.5 p-0 text-xs">{c.readings.map(r => <li key={r.binding}>
            <strong>{validity(r.value)}</strong> · {r.atom} · {r.freshness}{r.reason && ` · ${r.reason}`}{r.last_success && ` · last success ${r.last_success}`}</li>)}</ul>}
        {open[c.id] && <ul aria-label={`Evidence required for ${c.milestone}`} className="m-0 flex list-none flex-col gap-0.5 border-t border-[var(--table-rule)] p-0 pt-1.5 text-[13px] leading-[1.45]">
          {c.evidence.map((e, i) => <li key={i} className="flex gap-2"><span aria-hidden="true" className="text-muted-foreground">○</span>{e}</li>)}</ul>}
      </li>)}</ul>}
    {acc.state === 'ok' && <p className={note}>Criteria and the evidence they require, from the adopted plan view (plan v{view.plan_version}). A criterion reads Verified only when every binding on it reads true (PV2); with no binding it is Unknown, not 0.</p>}
  </section>
}

/** v3.6 Waiting on: what the module waits on (the module rows' rule) and what in this round's view waits on it. */
export function Dependencies({ view, item, row }: { view: PlanSnapshot; item: PlanNode; row: ModuleRow }) {
  const dependents = dependentsOf(view, item)
  return <section aria-label={`Dependencies of ${item.label}`} className={`${panel} gap-2.5`}>
    <CardHead title="Dependencies"><span className="font-mono text-[11px] text-muted-foreground">waiting_on · edges</span></CardHead>
    <section aria-label={`${item.label} waits on`} className="flex min-w-0 flex-col gap-1 text-sm">
      <h5 className="m-0 text-xs font-semibold text-muted-foreground">{item.label} waits on</h5>
      {row.waits.length > 0 ? <ul className="m-0 flex list-none flex-col gap-1 p-0">{row.waits.map(w => <li key={w.label} className="flex min-w-0 flex-col">
        <span><a href={w.href} className="inline-flex min-h-6 items-center underline-offset-2 hover:underline pointer-coarse:min-h-11">{w.label}</a>{w.status === 'Blocked' && <span> · Blocked</span>}</span>
        {w.reason && <span className="text-xs text-muted-foreground">{w.reason}</span>}
      </li>)}</ul>
        // waiting_on holds only the adopted view's blocking edges: none means no dependency waits, not that nothing
        // blocks; and only for an item the view document lists (row.inView), else its dependencies are not known.
        : <p className="m-0">{row.inView ? 'No dependency waiting' : `Dependencies Unknown · ${noViewReason(view)}`}</p>}
      {row.waits.some(w => w.status === 'Unknown') && <span className="text-xs text-muted-foreground">Milestone verification Unknown</span>}
    </section>
    <section aria-label={`Waits on ${item.label}`} className="flex min-w-0 flex-col gap-1.5 border-t border-[var(--table-rule)] pt-2.5 text-sm">
      <h5 className="m-0 text-xs font-semibold text-muted-foreground">Waits on {item.label}</h5>
      {dependents.state === 'unknown' ? <p className="m-0">Unknown · {dependents.reason}</p>
        : dependents.groups.length === 0 ? <p className="m-0">Nothing in {view.root}'s adopted plan view waits on {item.label}.</p>
        : <dl className="m-0 grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-x-2.5 gap-y-1.5">{dependents.groups.map(g => <div key={g.heading} data-dependents={g.heading} className="contents">
          <dt className="flex flex-col pt-0.5"><span className="whitespace-nowrap text-[11px] font-semibold uppercase tracking-[.04em] text-muted-foreground">{g.heading}</span>
            {g.status && <span className="whitespace-nowrap text-[11px] text-muted-foreground">{g.status}</span>}</dt>
          <dd className="m-0 min-w-0"><ul className="m-0 flex list-none flex-col gap-1 p-0">{g.entries.map(d => <li key={d.key} className="flex min-w-0 flex-col">
            <span><a href={d.href} className="underline-offset-2 hover:underline pointer-coarse:inline-flex pointer-coarse:min-h-11 pointer-coarse:items-center">{d.label}</a>{d.title && ` ${d.title}`}
              {!g.related && !g.status && <span className="text-muted-foreground"> · {d.status}</span>}</span>
            <span className="text-xs text-muted-foreground">{g.related ? `${d.status} · ${d.reason}` : `for ${d.requiredFor} — ${d.reason}`}</span>
          </li>)}</ul></dd>
        </div>)}</dl>}
      <p className={note}>Edges declared in {view.root}'s adopted plan view; other rounds' plans are not read here. No wait recorded: the edge is not in the plan view's waiting_on.</p>
    </section>
  </section>
}

/** v3.6 Live on: registered work on the module's steps now (plan-view working). */
export function Live({ view, item, scope, stale }: { view: PlanSnapshot; item: PlanNode; scope: ModuleScope; stale: boolean }) {
  const live = liveOn(view, scope)
  return <section aria-label={`Live on ${item.label}`} className={`${panel} gap-2`}>
    <CardHead title={`Live on ${item.label} · ${stale ? 'last read ' : ''}${live.length} registered`}><span className="font-mono text-[11px] text-muted-foreground">plan-view · working</span></CardHead>
    {live.length === 0 ? <p className="m-0 text-sm">{stale ? `Current activity is Unknown. Last read contained no registered work on ${item.label}.` : `No registered work on ${item.label} in this snapshot.`}</p>
      : <ul aria-label={`Work now on ${item.label}`} className="m-0 flex list-none flex-col gap-2 p-0">{live.map(w => <WorkRow key={w.external_work_id} work={w} view={view} stale={stale} />)}</ul>}
    <p className={note}>Registered external work on {item.label}'s steps · Unconfirmed after 60s without a heartbeat. Unregistered work is not seen ({view.root} coverage: {view.coverage.unregistered_work} unregistered · {view.coverage.reconciled ? 'Reconciled' : 'Not reconciled'}).</p>
  </section>
}

