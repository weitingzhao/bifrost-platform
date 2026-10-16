import { useEffect, useState } from 'react'
import { GoalForm } from '@/pages/shell/agents/GoalForm'
import { OwnerData } from './OwnerData'
import { Dot } from './OwnerParts'
import { OwnerSystem } from './OwnerSystem'
import { OwnerWork } from './OwnerWork'
import { OwnerYou } from './OwnerYou'
import { TONE_COLOR, ago, lastResultLine, type TabBadge, type Verdict } from './ownerModel'
import { OWNER_TABS, defaultOwnerTab, ownerBlocks, ownerTabFromHash, ownerTabHash, type OwnerTab } from './ownerTabs'
import { useNarrowScreen } from './useNarrowScreen'
import { useOwnerConsole, type OwnerConsoleData } from './useOwnerConsole'

const FOOTER_LINKS = [['#needs-you', 'Needs you'], ['#progress', 'Goals in progress'], ['#records', 'Records'], ['#status', 'Status'],
  ['#releases', 'Releases'], ['#infrastructure', 'Infrastructure'], ['#data', 'Data']] as const

const BADGE_STYLE: Record<string, string> = {
  attention: 'bg-[var(--color-lamp-yellow)] text-black',
  fail: 'bg-[var(--color-lamp-red)] text-white',
  info: 'bg-[var(--color-info)] text-white',
  muted: 'bg-[var(--control-fill)] text-foreground',
  unknown: 'bg-[var(--control-fill)] text-foreground',
}

function Badge({ badge }: { badge: TabBadge | undefined }) {
  if (!badge) return <span aria-hidden className="h-4 @2xl:hidden" />
  return <span className={`inline-flex h-4 min-w-4 items-center justify-center rounded-full px-1.5 text-[11px] font-bold leading-none @2xl:h-5 @2xl:min-w-5 @2xl:text-xs ${BADGE_STYLE[badge.tone] ?? BADGE_STYLE.muted}`}>{badge.text}</span>
}

/** Six tabs: equal cells while the page is narrow, pills once it is 42rem wide (the page's width, not the screen's: the sidebar takes a tablet's room). Sticky under the shell's top bar. */
function OwnerTabs({ tab, pick, badges }: { tab: OwnerTab; pick: (tab: OwnerTab) => void; badges: OwnerConsoleData['badges'] }) {
  return <nav aria-label="Owner Console sections" className="sticky top-12 z-10 -mx-3 border-b border-[var(--table-rule)] bg-background px-2 py-1.5 @2xl:px-3">
    <div className="grid grid-cols-6 gap-1 @2xl:flex @2xl:flex-wrap @2xl:gap-2">{OWNER_TABS.map(t => {
      const on = t.id === tab
      const badge = badges[t.id]
      return <button key={t.id} type="button" aria-pressed={on} onClick={() => pick(t.id)}
        aria-label={badge ? `${t.label}, ${badge.text}` : t.label}
        className={`flex min-h-[52px] min-w-0 flex-col items-center justify-center gap-1 rounded-[10px] border px-0.5 text-xs font-semibold @2xl:min-h-11 @2xl:flex-row @2xl:gap-2 @2xl:rounded-full @2xl:px-4 @2xl:text-sm ${on ? 'border-foreground bg-foreground text-background' : 'border-[var(--card-border)] bg-[var(--card-fill)] text-foreground'}`}>
        <span className="max-w-full truncate"><span className="@2xl:hidden">{t.short}</span><span className="hidden @2xl:inline">{t.label}</span></span>
        <Badge badge={badge} />
      </button>
    })}</div>
  </nav>
}

function VerdictBanner({ verdict, compact, goYou, updatedAt, now }: { verdict: Verdict; compact: boolean; goYou: () => void; updatedAt: number; now: number }) {
  const tinted = verdict.tone === 'attention'
  const box = tinted ? 'border-[var(--color-warning)] bg-[color-mix(in_srgb,var(--color-warning)_12%,transparent)]' : 'border-[var(--card-border)] bg-[var(--card-fill)]'
  if (compact) {
    return <button type="button" onClick={goYou} className={`flex min-h-11 items-center gap-2.5 rounded-[var(--card-radius)] border px-3 text-left ${box}`}>
      <Dot tone={verdict.tone} /><span className="flex-1 font-semibold">{verdict.headline}</span><span className="text-sm font-semibold" style={{ color: TONE_COLOR[verdict.tone] }}>See them ›</span>
    </button>
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
  return <section id="owner-chief" aria-label="Chief of staff" className="flex scroll-mt-28 flex-col gap-3">
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
 * The Owner Console (Owner 2026-10-16): the one page he keeps open on a phone, a tablet or a
 * desktop. A verdict first, then For you, the chief of staff, the work, the system and the data;
 * the tabs show one block at a time, All stacks them. Details stay on their own pages.
 */
export function OwnerConsolePage() {
  const narrow = useNarrowScreen()
  const [picked, setPicked] = useState<OwnerTab | null>(() => (typeof window === 'undefined' ? null : ownerTabFromHash(window.location.hash)))
  const tab = picked ?? defaultOwnerTab(narrow)
  const blocks = ownerBlocks(tab)
  const data = useOwnerConsole({ system: blocks.system, work: blocks.work, chief: blocks.chief })
  const [draft, setDraft] = useState({ text: '', key: 0 })

  useEffect(() => {
    const sync = () => setPicked(ownerTabFromHash(window.location.hash))
    window.addEventListener('hashchange', sync)
    return () => window.removeEventListener('hashchange', sync)
  }, [])

  /** Another tab starts at its top: the old scroll would leave its first block under the tab bar. */
  const pick = (next: OwnerTab, scrollTo = 'owner-console') => {
    setPicked(next)
    if (typeof window === 'undefined') return
    window.history.replaceState(null, '', ownerTabHash(next))
    requestAnimationFrame(() => document.getElementById(scrollTo)?.scrollIntoView?.({ block: 'start' }))
  }
  const makeGoal = (text: string) => {
    setDraft(d => ({ text, key: d.key + 1 }))
    pick(tab === 'all' ? 'all' : 'chief', 'owner-chief')
  }

  return <div id="owner-console" data-testid="owner-console" className="@container flex w-full min-w-0 scroll-mt-12 flex-col gap-5 pb-8">
    <OwnerTabs tab={tab} pick={pick} badges={data.badges} />
    <VerdictBanner verdict={data.verdict} compact={tab !== 'all' && tab !== 'you'} goYou={() => pick('you')} updatedAt={data.updatedAt} now={data.now} />
    {blocks.you && <OwnerYou data={data} onGoal={makeGoal} />}
    {/* Hidden, not unmounted, on other tabs: an edited draft and a goal just sent outlive a tab switch (review 224 B-224-1). */}
    <div hidden={!blocks.chief} className={blocks.chief ? "contents" : "hidden"}><OwnerChief data={data} draft={draft.text} draftKey={draft.key} /></div>
    {blocks.work && <OwnerWork data={data} />}
    {blocks.system && <OwnerSystem data={data} />}
    {blocks.data && <OwnerData data={data} />}
    <footer className="flex flex-wrap items-center gap-x-4 border-t border-[var(--table-rule)] pt-1 text-sm text-muted-foreground">
      <span>More:</span>{FOOTER_LINKS.map(([href, text]) => <a key={href} href={href} className="inline-flex min-h-11 items-center">{text}</a>)}
    </footer>
  </div>
}
