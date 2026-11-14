import { OpsSection } from '@/components/layout/OpsSection'
import { goalHref } from '@/lib/shell/consoleRoutes'
import { plainTitle } from '@/api/agentRuntimePlan'
import { CAPABILITY_LADDER, DEVELOPMENT_REVIEW, R4_HREF, R4_OUTCOMES } from './developmentModel'
import { useDevelopment } from './useDevelopment'

const card = 'min-w-0 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-3'
const link = 'inline-flex min-h-11 items-center text-sm underline'

export function CrewOverview({ now }: { now: number }) {
  const { read, refresh, refreshing } = useDevelopment(now)
  const recent = read.state === 'ready' ? read.progress.work.missions.filter(m => m.state === 'done').slice(0, 3) : []
  return <section id="overview" aria-label="Crew development overview" className="flex min-w-0 flex-col gap-4 [overflow-wrap:anywhere]">
    <OpsSection title="North star" description="Crew development · a direction, not an execution policy" bodyPadding="default">
      <p className="m-0 text-xl font-semibold">You set the goal. The crew turns it into useful results — with less effort and better returns over time.</p>
      <p className="mb-0 text-sm text-muted-foreground">You set priorities, rules and risk boundaries. The crew chooses how to work within them. Useful delivery and your time matter more than tokens consumed.</p>
      <a className={link} href="#crew?section=references&reference=blueprint">Platform north star and authority boundaries</a>
    </OpsSection>
    <div className="grid min-w-0 gap-4 @3xl:grid-cols-2">
      <section aria-label="Where we are" className={card}>
        <h3 className="m-0 text-base font-semibold">Where we are</h3>
        <p className="text-sm"><strong>Engineering collaboration demonstrated.</strong> Autonomous end-to-end delivery remains to be demonstrated.</p>
        <p className="text-xs text-muted-foreground">Historical conclusion · observed {DEVELOPMENT_REVIEW} · M-143</p>
        <p className="text-sm"><strong>Current availability:</strong> {read.state === 'ready' ? 'Runtime records read. Plan and task status alone do not establish delivery readiness.' : `Unknown · ${read.state}.`}</p>
        {read.state !== 'ready' && <p role={read.state === 'error' || read.state === 'offline' ? 'alert' : 'status'} className="text-sm">{read.reason}</p>}
        <p className="text-xs text-muted-foreground">Source: agent runtime /progress + /plan?root=R4.<br />Oldest successful source read: {read.observedAt ? new Date(read.observedAt).toISOString() : 'not observed'}.</p>
        {read.state === 'ready' && <p className="text-xs text-muted-foreground">Runtime generated: {read.progress.generated_at}. Read freshness limit: 2 minutes.</p>}
        <div className="flex flex-wrap gap-4"><button type="button" className={link} onClick={refresh} disabled={refreshing}>{refreshing ? 'Refreshing evidence…' : 'Refresh evidence'}</button><a className={link} href="#crew?section=capability-evidence">Current runtime gates</a></div>
      </section>
      <section aria-label="Next breakthrough" className={`${card} border-[var(--color-info)]`}>
        <p className="m-0 text-xs uppercase tracking-wide text-muted-foreground">Next breakthrough · Round 4.1</p>
        <h3 className="my-2 text-lg font-semibold">A small goal becomes a verified STG result.</h3>
        <p className="text-sm">No manual handoff between engineering acceptance, main and STG. You spend attention on goals and decisions.</p>
        <dl className="space-y-2 text-sm"><div><dt className="font-semibold">Already demonstrated</dt><dd className="m-0">Implementation, deterministic checks and independent review in M-143; main was merged manually.</dd></div>
          <div><dt className="font-semibold">Minimum gap to close</dt><dd className="m-0">Signed scope and credential boundaries; automatic landing; STG identity and health checks; traceable intervention evidence.</dd></div>
          <div><dt className="font-semibold">Pass when</dt><dd className="m-0">One authorized Console change completes that chain, with observed coordinating-thread writes equal to zero. Missing observation is unknown.</dd></div></dl>
        <p className="text-xs text-muted-foreground">Full-chain proof not established by this overview. Round 4.1 completion counts alone cannot pass this outcome.</p>
        <a className={link} href={R4_HREF}>Inspect Round 4 steps and blockers in Work</a>
      </section>
    </div>
    <OpsSection title="Capability ladder" description={`Evidence baseline · ${DEVELOPMENT_REVIEW}. Overlapping capabilities, not a maturity percentage.`} bodyPadding="default">
      <ol className="m-0 grid list-none gap-3 p-0 @3xl:grid-cols-3 @5xl:grid-cols-5">{CAPABILITY_LADDER.map((level, i) => <li key={level.title} className={card}>
        <span className="text-xs text-muted-foreground">Level {i + 1} · {level.proof}</span><h4 className="my-2 text-base font-semibold">{level.title}</h4>
        <p className="text-sm">{level.benefit}</p><p className="text-xs text-muted-foreground"><strong>Needs evidence:</strong> {level.gap}</p>
        <p className="text-xs">Round {level.rounds}</p><a className={link} href={level.href}>Inspect evidence</a>
      </li>)}</ol>
    </OpsSection>
    <OpsSection title="Results and their limits" bodyPadding="default">
      <p className="text-sm"><a href={goalHref('M-143')}>M-143 · engineering collaboration</a> — Codex implementation, deterministic validation and Cursor review accepted. The coordinating thread merged main manually. Engineering time is not end-to-end delivery time.</p>
      <p className="text-xs text-muted-foreground">Historical snapshot · {DEVELOPMENT_REVIEW}. It survives a runtime outage; it does not prove present availability.</p>
      <h4 className="text-sm font-semibold">Completed goals in the current runtime summary</h4>
      {read.state !== 'ready' ? <p className="text-sm">Current results unknown until a complete fresh read succeeds.</p> : recent.length ? <ul className="space-y-2 pl-5 text-sm">{recent.map(m => <li key={m.mission_id}><a href={goalHref(m.mission_id)}>{m.mission_id} · {m.title || m.goal || 'Goal'}</a> · {m.state}</li>)}</ul> : <p className="text-sm">No completed goals in this summary window. This is not the full work history.</p>}
      <p className="text-xs text-muted-foreground">A completed goal is not proof of production use or business value. <a href="#records?tab=work">Full work history</a></p>
    </OpsSection>
    <OpsSection title="Round 4 · what each step enables" description="Stable outcome mapping; execution states come from the same runtime plan used by Work." bodyPadding="default">
      {read.state === 'ready' && read.plan.round.ledgerUnread && <p role="status" className="text-sm">Imported ledger evidence could not be read: {read.plan.round.ledgerUnread}. Counts remain runtime readings.</p>}
      <div className="grid gap-3 @3xl:grid-cols-2">{Object.entries(R4_OUTCOMES).map(([key, outcome]) => {
        const item = read.state === 'ready' ? read.plan.round.items.find(i => i.key === key) : undefined
        return <section key={key} aria-label={`Round ${key}`} className={card}><h4 className="m-0 text-sm font-semibold">{key}{item ? ` · ${plainTitle(item.name)}` : ''}</h4>
          <p className="text-sm">{outcome}</p><p className="text-xs text-muted-foreground">{item ? `Plan state: ${item.state} · ${item.done}/${item.total} steps done · ${item.manual ?? 'unknown'} manual · ${item.stopped ?? 'unknown'} stopped` : read.state === 'ready' ? 'No matching item in this runtime plan.' : `Plan state unknown · ${read.state}`}</p>
          <a className={link} href={R4_HREF}>Plan, sign-offs and step evidence</a></section>
      })}</div>
      <p className="text-sm">Wave 2 starts after 4.1 is on PROD; Wave 3 follows the second repository. This view does not authorize either wave.</p>
    </OpsSection>
    <details className={`${card} text-sm`}><summary className="min-h-11 cursor-pointer py-3">Basis, boundaries and ROI evidence</summary>
      <p>Discussion baseline: VISION-institution-and-roi-2026-10-24.md §1, §6 and §9; historical observation at {DEVELOPMENT_REVIEW}. Round 4 mapping: WORK.md, W-31, Round 4. These are source descriptions, not live state.</p>
      <p>Platform D6 remains the Console/API operations north star. D10 trading execution remains blocked. Qualification never grants permission.</p>
      <p>ROI improvement is not yet established here. Compare useful deliveries, quality, rework, end-to-end time, Owner effort and governance cost on similar work. Research inference costs remain separate from Crew engineering costs.</p>
      <a className={link} href="#crew?section=references&reference=roadmap">Roadmap sources and current rules</a>
    </details>
  </section>
}
