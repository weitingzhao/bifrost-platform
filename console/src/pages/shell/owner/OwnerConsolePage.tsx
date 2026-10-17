import { useState } from 'react'
import { DetailPageLinks } from '@/components/shell/DetailPageLinks'
import { formatShellHash, type EntryRouteId } from '@/lib/shell/consoleRoutes'
import { GoalForm } from '@/pages/shell/agents/GoalForm'
import { OwnerData } from './OwnerData'
import { Dot } from './OwnerParts'
import { OwnerSystem } from './OwnerSystem'
import { OwnerWork } from './OwnerWork'
import { OwnerYou } from './OwnerYou'
import { TONE_COLOR, ago, lastResultLine, type Verdict } from './ownerModel'
import { useOwnerConsole, type OwnerConsoleData } from './useOwnerConsole'

/** Which blocks an entry shows. System carries the data tiles and the way into every detail page. */
function entryBlocks(entry: EntryRouteId) {
  return { you: entry === 'you', chief: entry === 'chief', work: entry === 'work', system: entry === 'system' }
}

function VerdictBanner({ verdict, compact, updatedAt, now }: { verdict: Verdict; compact: boolean; updatedAt: number; now: number }) {
  const tinted = verdict.tone === 'attention'
  const box = tinted ? 'border-[var(--color-warning)] bg-[color-mix(in_srgb,var(--color-warning)_12%,transparent)]' : 'border-[var(--card-border)] bg-[var(--card-fill)]'
  if (compact) {
    return <a href={formatShellHash('you')} className={`flex min-h-11 items-center gap-2.5 rounded-[var(--card-radius)] border px-3 text-left text-foreground no-underline ${box}`}>
      <Dot tone={verdict.tone} /><span className="flex-1 font-semibold">{verdict.headline}</span><span className="text-sm font-semibold" style={{ color: TONE_COLOR[verdict.tone] }}>See them ›</span>
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

function OwnerChief({ data, draft, draftKey }: { data: OwnerConsoleData; draft: string; draftKey: number }) {
  const last = data.last
  return <section id="owner-chief" aria-label="Chief of staff" className="flex flex-col gap-3">
    <h2 className="m-0 text-lg font-semibold">Chief of staff</h2>
    <div className="grid gap-4 @3xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
      <GoalForm key={draftKey} draft={draft} />
      <div className="flex min-w-0 flex-col gap-2 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-4 text-sm">
        <span className="text-xs uppercase tracking-wide text-muted-foreground">Its latest</span>
        {last ? <p className="m-0 break-words"><strong>{lastResultLine(last, data.now)}</strong>{data.lastGoal && ` · ${data.lastGoal.split('\n')[0]}`}</p>
          : <p className="m-0 text-muted-foreground">{data.runtimeError ? 'The agent runtime could not be read.' : data.work ? 'No implementation accepted recently.' : 'Reading the agent runtime…'}</p>}
        {last && <a href={`#progress?goal=${encodeURIComponent(last.mission)}`}>What it wrote and did</a>}
        <p className="m-0 text-muted-foreground">Morning and evening briefings, and asking it a question, come with the agents' inbox (plan step 2.3).</p>
      </div>
    </div>
  </section>
}

/**
 * The Owner's four entries (Owner 2026-10-16; design system v2 §10): For you, Chief of staff,
 * Work and System are one page each, with the verdict first — whole on For you, one line that
 * goes there on the others. The shell keeps this component mounted while the Owner moves between
 * entries, so a goal drafted from "Make it a goal" is still in the chief's box on arrival.
 * Details stay on their own pages.
 */
export function OwnerConsolePage({ entry = 'you' }: { entry?: EntryRouteId }) {
  const blocks = entryBlocks(entry)
  const data = useOwnerConsole({ system: blocks.system, work: blocks.work, chief: blocks.chief })
  const [draft, setDraft] = useState({ text: '', key: 0 })

  const makeGoal = (text: string) => {
    setDraft(d => ({ text, key: d.key + 1 }))
    if (typeof window !== 'undefined') window.location.hash = formatShellHash('chief')
  }

  return <div id="owner-console" data-testid="owner-console" data-entry={entry} className="@container flex w-full min-w-0 flex-col gap-5 pb-8">
    <VerdictBanner verdict={data.verdict} compact={entry !== 'you'} updatedAt={data.updatedAt} now={data.now} />
    {blocks.you && <OwnerYou data={data} onGoal={makeGoal} />}
    {/* Hidden, not unmounted, on the other entries: an edited draft and a goal just sent outlive moving away (review 224 B-224-1). */}
    <div hidden={!blocks.chief} className={blocks.chief ? 'contents' : 'hidden'}><OwnerChief data={data} draft={draft.text} draftKey={draft.key} /></div>
    {blocks.work && <OwnerWork data={data} />}
    {blocks.system && <OwnerSystem data={data} />}
    {blocks.system && <OwnerData data={data} />}
    {blocks.system && <DetailPageLinks />}
  </div>
}
