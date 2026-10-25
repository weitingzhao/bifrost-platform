import { DomainSection } from '@/components/shell/DomainSection'
import { CapabilityEvidence } from '../agents/CapabilityEvidence'
import { ServiceVersions } from '../services/ServiceVersions'
import { SatelliteHealthPage } from '@/pages/SatelliteHealthPage'
import { OwnerReleaseLane } from './OwnerReleaseLane'
import { ChiefConversation } from './ChiefConversation'
import { useEffect, useState } from 'react'
import { GovernanceReferences } from '../progress/GovernanceReferences'
import { PoolReadings } from './PoolReadings'
import { OwnerQuota } from './OwnerQuota'
import { OwnerCapabilities } from './OwnerCapabilities'
import { canonicalQuery, scrollAfterShell } from '@/lib/shell/useHashSection'
import { formatShellHash, goalHref, type EntryRouteId } from '@/lib/shell/consoleRoutes'
import { GoalForm } from '@/pages/shell/agents/GoalForm'
import { ProgressPage } from '@/pages/shell/progress/ProgressPage'
import { OwnerData } from './OwnerData'
import { Dot } from './OwnerParts'
import { OwnerSystem } from './OwnerSystem'
import { RuntimeHealthNotice } from './RuntimeHealthNotice'
import { OwnerWaiting } from './OwnerWaiting'
import { OwnerWork } from './OwnerWork'
import { OwnerYou } from './OwnerYou'
import { TONE_COLOR, ago, lastResultLine, runtimeUnreadLine, type Verdict } from './ownerModel'
import { useOwnerConsole, type OwnerConsoleData } from './useOwnerConsole'

/** Each entry mounts its own facts; shared Command drafts remain mounted across routes. */
function entryBlocks(entry: EntryRouteId | null) {
  return { you: entry === 'command', chief: entry === 'command', work: entry === 'work', system: entry === 'system', crew: entry === 'crew', services: entry === 'services' }
}

function VerdictBanner({ verdict, compact, updatedAt, now }: { verdict: Verdict; compact: boolean; updatedAt: number; now: number }) {
  const tinted = verdict.tone === 'attention'
  const box = tinted ? 'border-[var(--color-warning)] bg-[color-mix(in_srgb,var(--color-warning)_12%,transparent)]' : 'border-[var(--card-border)] bg-[var(--card-fill)]'
  if (compact) {
    return <a href={formatShellHash('command')} className={`flex min-h-11 items-center gap-2.5 rounded-[var(--card-radius)] border px-3 text-left text-foreground no-underline ${box}`}>
      <Dot tone={verdict.tone} /><span className="flex-1 font-semibold">{verdict.headline}</span><span className="text-sm font-semibold" style={{ color: TONE_COLOR[verdict.tone] }}>{tinted ? 'See them ›' : 'Command ›'}</span>
    </a>
  }
  return <section aria-label="Verdict" data-owner-verdict={verdict.tone} className={`flex items-center gap-3 rounded-[var(--card-radius)] border px-4 py-3 ${box}`}>
    <Dot tone={verdict.tone} size="size-3.5" />
    <div className="flex min-w-0 flex-col gap-0.5">
      <strong className="text-lg @xl:text-xl">{verdict.headline}</strong>
      <span className="break-words text-sm text-muted-foreground">{verdict.line}</span>
      {updatedAt > 0 && <span className="font-mono text-xs text-muted-foreground" title={new Date(updatedAt).toISOString()}>as of {ago(updatedAt, now)}</span>}
    </div>
  </section>
}

function OwnerChief({ data, draft, draftKey, active }: { active: boolean; data: OwnerConsoleData; draft: string; draftKey: number }) {
  const last = data.last
  return <section id="owner-chief" aria-label="Chief of staff" className="flex flex-col gap-3">
    <h2 className="m-0 text-lg font-semibold">Chief of staff</h2>
    <ChiefConversation active={active} />
    <div className="grid gap-4 @3xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
      <div id="new-goal"><GoalForm key={draftKey} draft={draft} onCreated={result => { window.location.hash = goalHref(result.mission_id) }} /></div>
      <div className="ops-chief-latest flex min-w-0 flex-col gap-2 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-4 text-sm">
        <span className="text-xs uppercase tracking-wide text-muted-foreground">Its latest</span>
        {last ? <p className="m-0 break-words"><strong>{lastResultLine(last, data.now)}</strong>{data.lastGoal && ` · ${data.lastGoal.split('\n')[0]}`}</p>
          : <p className="m-0 text-muted-foreground">{data.runtimeError ? runtimeUnreadLine(data.runtimeReadAt, data.runtimeOpenedAt, data.now) : data.work ? 'No implementation accepted recently.' : 'Reading the agent runtime…'}</p>}
        {last && <a href={goalHref(last.mission)}>What it wrote and did</a>}
      </div>
    </div>
  </section>
}

/** Command keeps conversation and goal drafts mounted across domains and detail pages. */
export function OwnerConsolePage({ entry = 'command' }: { entry?: EntryRouteId | null }) {
  useEffect(() => {
    let cancel = () => {}
    const scroll = () => {
      const section = new URLSearchParams(canonicalQuery(window.location.hash)).get('section')
      if (!section || (entry !== 'command' && entry !== 'crew')) return
      cancel(); cancel = scrollAfterShell(() => document.getElementById(section))
    }
    scroll(); window.addEventListener('hashchange', scroll)
    return () => { cancel(); window.removeEventListener('hashchange', scroll) }
  }, [entry])
  const blocks = entryBlocks(entry)
  const data = useOwnerConsole({ system: blocks.system, services: blocks.services, work: blocks.work, chief: blocks.chief })
  const [draft, setDraft] = useState({ text: '', key: 0 })

  const makeGoal = (text: string) => {
    setDraft(d => ({ text, key: d.key + 1 }))
    if (typeof window !== 'undefined') window.location.hash = formatShellHash('command', 'section=new-goal')
  }

  return <div id="owner-console" data-testid="owner-console" data-entry={entry} className="@container flex w-full min-w-0 flex-col gap-5 pb-8">
    <VerdictBanner verdict={data.verdict} compact={entry !== 'command'} updatedAt={data.updatedAt} now={data.now} />
    {blocks.you && <OwnerYou data={data} onGoal={makeGoal} />}
    {blocks.you && <OwnerWaiting />}
    {/* Hidden, not unmounted, on the other entries: an edited draft and a goal just sent outlive moving away (review 224 B-224-1). */}
    <div hidden={!blocks.chief} className={blocks.chief ? 'contents' : 'hidden'}><OwnerChief active={blocks.chief} data={data} draft={draft.text} draftKey={draft.key} /></div>
    {(blocks.work || blocks.system) && <RuntimeHealthNotice progress={data.runtime} now={data.now} />}
    {blocks.work && <OwnerWork data={data} />}
    {blocks.work && <><OwnerReleaseLane data={data} /><ProgressPage /></>}
    {blocks.system && <><OwnerSystem data={data} /><OwnerData data={data} infrastructure /></>}
    {blocks.services && <><OwnerData data={data} /><DomainSection id="versions" label="Running versions"><ServiceVersions /></DomainSection><DomainSection id="applications" label="Applications"><SatelliteHealthPage lockToViewer /></DomainSection></>}
    {blocks.crew && <>
      <section id="roles" aria-label="Roles and performance"><OwnerCapabilities now={data.now} /></section>
      <section id="capacity" aria-label="Capacity and cost"><PoolReadings now={data.now} /><OwnerQuota now={data.now} /></section>
      <DomainSection id="capability-evidence" label="Capability evidence"><CapabilityEvidence /></DomainSection>
      <GovernanceReferences />
    </>}
  </div>
}
