import type { UseQueryResult } from '@tanstack/react-query'
import type { PlanSnapshot } from '@/api/planSnapshot'
import { TONE_COLOR, type Tone } from '../owner/ownerModel'
import { useNow } from '../progress/progressClock'
import { moduleRows } from './planModuleModel'
import { roundLabel } from './planRoundModel'
import { responsibility, validity } from './planSnapshotModel'
import type { PlanAddress } from './usePlanSnapshot'
import { workTabHref } from './workViewModel'
import { NeedsCleanupEntry } from './NeedsCleanup'
import { cleanupBreakdown } from './needsCleanupModel'
import { useNeedsCleanup } from './useNeedsCleanup'
import { answeredWords, progressOld, sinceWords as since } from './readAge'
import { CLASS_MEANINGS, OTHER_ROUNDS, dependencyWaits, dependencyWords, mainlineLabels, mainlineSentence, othersSentence, roundHeader, signoffBreakdown, signoffGaps, signoffWords, summaryLamp, type SignoffCount } from './workSummaryModel'

const TONE_WORDS: Partial<Record<Tone, string>> = { ok: 'Nothing for you, as far as read', attention: 'Needs a look', unknown: 'Not fully known' }
const clock = (at: number) => new Date(at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })

function Lamp({ tone, why = [] }: { tone: Tone; why?: string[] }) {
  const color = TONE_COLOR[tone]
  const words = [TONE_WORDS[tone], ...why].filter(Boolean).join(': ')
  return <span role="img" aria-label={words} title={words} data-summary-tone={tone}
    className="size-[11px] flex-none rounded-full" style={{ background: color, boxShadow: `0 0 0 4px color-mix(in srgb, ${color} 22%, transparent)` }} />
}

const pill = 'inline-flex h-5 items-center rounded-full border border-dashed border-[var(--color-lamp-gray)] px-[7px] text-[11px] font-semibold text-muted-foreground'
const box = 'flex min-w-0 flex-col gap-2 rounded-[20px] border border-[var(--ops-edge)] px-4 py-3.5 shadow-[var(--ops-shadow)] [background:var(--ops-panel)] [overflow-wrap:anywhere]'

/**
 * Work → the round before a plan view is read: which round is not known yet, cannot be, or the runtime holds none;
 * "none" only from a round list that is now (R5:5.5-4: GET /progress older than twice its 5 s poll is as of then).
 */
function NoRound({ plan, now }: { plan: PlanAddress; now: number }) {
  const { rounds } = plan
  if (rounds.state === 'reading') return <p role="status" className="m-0">Reading which round is newest…</p>
  if (rounds.state === 'unknown') return <div role="alert" className="rounded-[var(--control-radius)] border border-[var(--color-warning)] p-3">
    Newest round Unknown · {rounds.reason}. This plan and live view shows no round until it is known; an address can still name a round (<code>#work?root=…</code>).
    <button className="ml-2 min-h-11 rounded-[var(--control-radius)] border px-3" onClick={plan.retry}>Retry</button></div>
  if (rounds.failed || progressOld(rounds.at, now)) return <p role="status" className="m-0">
    The runtime held no round as of {new Date(rounds.at).toISOString()}; whether it holds one now is Unknown · GET /progress {rounds.failed ? `failed (${rounds.failed})` : answeredWords(rounds.at, now)}.</p>
  return <p className="m-0">The runtime holds no round yet.</p>
}

/** The plan view could not be read: why, the last snapshot still shown as Stale (or none yet), and Retry. */
export function PlanReadFailure({ query }: { query: UseQueryResult<PlanSnapshot> }) {
  const view = query.data
  const reason = query.isPaused ? 'Offline' : query.error?.message ?? ''
  return <div role="alert" className="rounded-[var(--control-radius)] border border-[var(--color-warning)] p-3">
    Unknown · {reason}. {view ? `Stale · showing the last successful snapshot from ${view.as_of}.` : 'No successful snapshot yet.'}
    <button className="ml-2 min-h-11 rounded-[var(--control-radius)] border px-3" onClick={() => void query.refetch()}>Retry</button></div>
}

/**
 * The top of Work (Design v3.6 WorkPage): where the round stands in one sentence, the mainline, the sign-offs, the
 * dependency waits and the ETA, each computed from plan-view and GET /plan (workSummaryModel.ts), with when it was read,
 * and the one way to what needs cleanup (R5:5.5-4). How each part is read is one fold away; a plan view that cannot be
 * read says so, with the last one marked Stale.
 */
export function WorkSummaryCard({ plan, query, count }: { plan: PlanAddress; query: UseQueryResult<PlanSnapshot>; count: SignoffCount }) {
  const now = useNow()
  const view = query.data
  const failed = query.isError || query.isPaused
  const failure = failed && <PlanReadFailure query={query} />
  // What needs cleanup, from the shell's GET /progress and this plan view: no request of its own (R5:5.5-4).
  const cleanup = useNeedsCleanup(plan, query, now)
  if (!view) {
    return <section aria-label="Where the round stands" className={box}>
      <div className="flex flex-wrap items-center gap-2"><Lamp tone="unknown" />
        <span className="text-xs font-semibold uppercase tracking-[.08em] text-muted-foreground">{plan.root ? roundLabel(plan.root) : 'Work'}</span></div>
      {plan.root === null ? <NoRound plan={plan} now={now} /> : !failed && <p role="status" className="m-0">Loading plan and live work…</p>}
      {failure}
      {/* The hosts of Needs cleanup do not wait on the plan view: its one entry is here too (R5:5.5-4). */}
      <span className="text-[13px] text-muted-foreground"><NeedsCleanupEntry plan={plan} cleanup={cleanup} /></span>
    </section>
  }
  const rows = moduleRows(view)
  const stale = failed || now - Date.parse(view.as_of) > 60_000 || view.freshness.database.status !== 'Fresh'
  const mainline = mainlineSentence(view, rows)
  const others = othersSentence(rows, view.mainline.current?.plan_node_id ?? null)
  const labels = mainlineLabels(view)
  const waits = dependencyWaits(view, rows)
  const lamp = summaryLamp({ stale, mainlineUnknown: mainline.unknown, count, rows })
  const named = plan.from === 'address'
  const read = query.dataUpdatedAt > 0 ? query.dataUpdatedAt : Date.parse(view.as_of)
  const current = view.nodes.find(n => n.plan_node_id === view.mainline.current?.plan_node_id)
  return <section aria-label="Where the round stands" data-summary className={box}>
    <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
      <Lamp tone={lamp.tone} why={lamp.why} />
      <span className="mr-auto text-xs font-semibold uppercase tracking-[.08em] text-muted-foreground">{roundHeader(view)}</span>
      <span className="font-mono text-[11px] text-muted-foreground" title={`Last successful read ${new Date(read).toISOString()} · snapshot as of ${view.as_of}`}>
        {stale ? `Stale · last read ${clock(read)}` : `plan-view ${view.root} · read ${since(read, now)} ago`}</span>
    </div>
    <p data-summary-sentence className="m-0 text-lg font-[650] leading-[1.35] tracking-[-.01em] [text-wrap:pretty] @2xl:text-xl">{mainline.text}{others && ` ${others}`}</p>
    <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 text-[13px] text-muted-foreground">
      <span data-summary-mainline>{labels.length ? `Mainline ${labels.join(' → ')}` : 'Mainline Unknown'}</span>
      <a href={workTabHref('signoffs', plan.root, named)} data-summary-signoffs className="text-inherit underline-offset-2 hover:underline"
        title={signoffBreakdown(count).join('\n')}>
        Sign-offs: {signoffWords(count)}{signoffGaps(count).length > 0 && ` · ${signoffGaps(count).join('; ')}`}
        {count.tasks.length > 0 && ` · ${count.tasks.length} task${count.tasks.length === 1 ? ' waits' : 's wait'} on you`}
        {count.uncounted.length > 0 && ` · ${count.uncounted.join(', ')} acceptance not counted here`}
        {count.scope === 'round' && ` · ${OTHER_ROUNDS}`}</a>
      <span data-summary-waits title={waits.state === 'ok' ? `Blocking dependencies whose milestone is not verified: ${waits.blocked} Blocked, ${waits.unverified} Unknown` : undefined}>
        {dependencyWords(waits)}</span>
      <span className="inline-flex items-center gap-1.5">ETA <span className={pill} title={view.eta.reason}>{view.eta.status}</span></span>
      {/* What was left stale (offline hosts, registered work with no heartbeat) is a step away on Activity (R5:5.5-4). */}
      <NeedsCleanupEntry plan={plan} cleanup={cleanup} />
    </div>
    <details className="text-[13px]">
      <summary className="cursor-pointer text-muted-foreground pointer-coarse:min-h-11 pointer-coarse:py-3">How this is read</summary>
      <ul className="m-0 mt-1 flex list-none flex-col gap-1 p-0">
        {view.next.length === 0 ? <li>Next · Unknown</li> : view.next.map((next, i) => <li key={i}>Next · {next.action} · {responsibility(view.nodes.find(n => n.plan_node_id === next.plan_node_id))}</li>)}
        <li>Mainline basis · {view.mainline.basis}{view.mainline.milestone_status && ` · Milestones ${view.mainline.milestone_status}`} · Verification {view.freshness.verification}
          {current?.label && ` · stands at ${current.label}: ${view.mainline.basis === 'Versioned acceptance observations' ? 'the first mainline item not verified accepted' : 'the first mainline item whose steps are not all done'}`}</li>
        {view.mainline.current?.current_validity && <li>Acceptance of {current?.label ?? view.mainline.current.plan_node_id} · {validity(view.mainline.current.current_validity)}</li>}
        <li>Lamp · {TONE_WORDS[lamp.tone]}: {lamp.why.join('; ')}.</li>
        <li>Items by runtime step state (nodes[].state, the controller's record of each step):
          <ul className="m-0 list-disc pl-5">{CLASS_MEANINGS.map(([name, meaning]) => <li key={name}>“{name}”: {meaning}</li>)}</ul></li>
        {signoffBreakdown(count).map(line => <li key={line}>Sign-offs · {line}</li>)}
        {waits.state === 'ok' && <li>Dependency waits · {waits.blocked} Blocked · {waits.unverified} milestone Unknown</li>}
        {cleanupBreakdown(cleanup).map(line => <li key={line}>Needs cleanup · {line}</li>)}
        <li>ETA · {view.eta.status} · {view.eta.reason}</li>
        {view.warnings.map(w => <li key={w}>{w}</li>)}
        <li>Plan version {view.plan_version} · {view.artifact_id || 'No adopted view'}</li>
        <li>{view.freshness.external_work}</li>
        <li>Database {view.freshness.database.status} · observed {view.freshness.database.observed_at} · Last successful snapshot {view.as_of}</li>
      </ul>
    </details>
    {failure}
  </section>
}
