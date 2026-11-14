import { AgentThreadsInProgress } from '../status/AgentThreadsInProgress'
import { useEffect } from 'react'
import { OpsSection } from '@/components/layout/OpsSection'
import { CodeHealthPage } from '@/pages/CodeHealthPage'
import { CommitLineagePage } from '@/pages/CommitLineagePage'
import { formatShellHash, hashQueryParam } from '@/lib/shell/consoleRoutes'
import { canonicalQuery, scrollAfterShell } from '@/lib/shell/useHashSection'

/** One of Work's pages a step away from the round (R5:5.5-2). */
export type ProgressPart = 'activity' | 'code-health' | 'commit-lineage'

/**
 * Work's detail pages, one part at a time: the thread activity, the commit lineage or the code health. The goal
 * cards and the overview that were here showed missions and tasks of the dispatch lane and went with it
 * (LANE-BIZ-ALIGN, 2026-11-13).
 */
export function ProgressPage({ part }: { part: ProgressPart }) {
  // One scroll per address, on arrival and on every hash change, to the section the address names.
  useEffect(() => {
    let cancel = () => {}
    const go = () => {
      const section = hashQueryParam(canonicalQuery(window.location.hash), 'section')
      if (!section) return
      cancel()
      cancel = scrollAfterShell(() => document.getElementById(section))
    }
    go()
    window.addEventListener('hashchange', go)
    return () => { cancel(); window.removeEventListener('hashchange', go) }
  }, [])
  return <div className="flex w-full min-w-0 flex-col gap-4 overflow-x-hidden" data-testid="merged-progress">
    {part === 'activity' && <>
      <section id="activity" aria-label="Activity"><AgentThreadsInProgress /><a href="#infrastructure?section=hosts">Host health and capacity · System</a></section>
      <section aria-label="History"><OpsSection title="History" bodyPadding="compact">
        <p className="m-0 text-sm">The runtime's event timeline is in <a href={formatShellHash('records', 'tab=work')}>Records › Work history</a>.</p>
      </OpsSection></section>
    </>}
    {part === 'commit-lineage' && <section aria-label="Commit lineage" className="flex w-full min-w-0 flex-col"><div id="commit-lineage" className="flex w-full min-w-0 flex-col"><CommitLineagePage /></div></section>}
    {part === 'code-health' && <section aria-label="Code health" className="flex w-full min-w-0 flex-col"><div id="code-health" className="flex w-full min-w-0 flex-col"><CodeHealthPage /></div></section>}
  </div>
}
