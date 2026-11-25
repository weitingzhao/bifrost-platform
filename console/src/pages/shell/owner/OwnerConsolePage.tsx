import { DomainVerdictStrip } from '@/components/shell/DomainVerdictStrip'
import { CollapsibleSection } from '@/components/shell/CollapsibleSection'
import { LawRegister } from './LawRegister'
import { LawEffect } from './LawEffect'
import { OwnerIssues } from './OwnerIssues'
import { AuditConclusions } from './AuditConclusions'
import { DomainSection } from '@/components/shell/DomainSection'
import { CapabilityEvidence } from '../agents/CapabilityEvidence'
import { ServiceVersions } from '../services/ServiceVersions'
import { SatelliteHealthPage } from '@/pages/SatelliteHealthPage'
import { useEffect } from 'react'
import { GovernanceReferences } from '../progress/GovernanceReferences'
import { canonicalQuery, scrollAfterShell } from '@/lib/shell/useHashSection'
import { formatShellHash, type EntryRouteId } from '@/lib/shell/consoleRoutes'
import { useWorkView } from '@/pages/shell/agents/workViewModel'
import { CrewInstruments } from './CrewInstruments'
import { OwnerData } from './OwnerData'
import { HarnessPreview } from './HarnessPreview'
import { OwnerGpuPower } from './OwnerGpuPower'
import { Dot } from './OwnerParts'
import { OwnerSystem } from './OwnerSystem'
import { RuntimeHealthNotice } from './RuntimeHealthNotice'
import { OwnerWaiting } from './OwnerWaiting'
import { OwnerWork } from './OwnerWork'
import { OwnerForYourInfo, OwnerYou } from './OwnerYou'
import { TONE_COLOR, ago, type Verdict } from './ownerModel'
import { useOwnerConsole } from './useOwnerConsole'
import { current } from './useOwnerYours'

/** Each entry mounts its own facts; shared Command drafts remain mounted across routes. */
function entryBlocks(entry: EntryRouteId | null) {
  return { you: entry === 'command', work: entry === 'work', system: entry === 'system', crew: entry === 'crew', services: entry === 'services' }
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

/** The Owner's console: one entry at a time, each mounting its own facts. */
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
  // Work's runtime-wide reads (release runs, the coordinator's note, the last result) only while its Activity page is open (R5:5.5-2).
  const workView = useWorkView()
  const activity = blocks.work && workView.kind === 'section' && workView.section === 'activity'
  const data = useOwnerConsole({ system: blocks.system, services: blocks.services, work: activity })
  return <div id="owner-console" data-testid="owner-console" data-entry={entry} className="@container flex w-full min-w-0 flex-col gap-5 pb-8">
    <VerdictBanner verdict={data.verdict} compact={entry !== 'command'} updatedAt={data.updatedAt} now={data.now} />
    {blocks.you && <HarnessPreview />}
    {blocks.you && <OwnerGpuPower nodes={data.nodes} openApprovals={current(data.needsYou.query)} recentApprovals={current(data.needsYou.handledQuery)} now={data.now} />}
    {blocks.you && <OwnerYou data={data} />}
    {/* Owner 2026-11-24: the laws up (right after what he approves), For your info down; every section folds, open by default. */}
    {blocks.you && <CollapsibleSection id="laws" label="Laws"><LawRegister /></CollapsibleSection>}
    {/* Owner 2026-11-25: laws and complaints apart, and a place of its own for whether a law eased the complaints under it. */}
    {blocks.you && <CollapsibleSection id="law-effect" label="Law effect"><LawEffect /></CollapsibleSection>}
    {blocks.you && <CollapsibleSection id="issues" label="Owner's issues"><OwnerIssues /></CollapsibleSection>}
    {blocks.you && <OwnerWaiting />}
    {/* The audit's conclusions on those laws' standards (箭10 2026-11-24): what the audit found, beside the laws. */}
    {blocks.you && <CollapsibleSection id="audit" label="Audit conclusions"><AuditConclusions /></CollapsibleSection>}
    {blocks.you && <CollapsibleSection id="for-your-info" label="For your info"><OwnerForYourInfo data={data} /></CollapsibleSection>}
    {(blocks.work || blocks.system) && <RuntimeHealthNotice progress={data.runtime} now={data.now} />}
    {blocks.work && <OwnerWork data={data} />}
    {blocks.system && <><OwnerSystem data={data} /><OwnerData data={data} infrastructure /></>}
    {/* Each entry leads with its own verdict (箭10 2026-11-23): Services and Crew from their domain's lights. */}
    {blocks.services && <><DomainVerdictStrip title="Services" domain="payload" /><OwnerData data={data} /><DomainSection id="versions" label="Running versions"><ServiceVersions /></DomainSection><DomainSection id="applications" label="Applications"><SatelliteHealthPage lockToViewer /></DomainSection></>}
    {blocks.crew && <>
      <DomainVerdictStrip title="Crew" domain="crew" />
      <DomainSection id="capability-evidence" label="Capability evidence"><CapabilityEvidence /></DomainSection>
      <DomainSection id="instruments" label="Laws, skills & tools"><CrewInstruments /></DomainSection>
      <GovernanceReferences />
    </>}
  </div>
}
