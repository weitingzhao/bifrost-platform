import { useQuery } from '@tanstack/react-query'
import { useRef } from 'react'
import { AGENT_RUNTIME_POLL_MS, fetchRuntimeProgress } from '@/api/agentRuntime'
import { NowClock, Tick, relative } from './progressClock'
import { ProgressTimeline } from './ProgressTimeline'
import { runtimeUnreadLine } from '../owner/ownerModel'

/**
 * Finished work, on Records: the runtime's event timeline (steps done, decisions, plan changes, controls, releases).
 * The goal, task and attempt lists that were here went with the dispatch lane (LANE-BIZ-ALIGN, 2026-11-13).
 */
export function ProgressHistory() {
  const openedAt = useRef(Date.now())
  const runtimeQ = useQuery({ queryKey: ['agent-runtime-progress'], queryFn: ({ signal }) => fetchRuntimeProgress(signal), refetchInterval: AGENT_RUNTIME_POLL_MS, retry: false })
  const runtime = runtimeQ.data
  // History stays as last read when a read fails or is paused offline, and says so with when (review 488 A1, B1).
  const unreadable = runtimeQ.isError || runtimeQ.isPaused
  const readAt = runtimeQ.dataUpdatedAt > 0 ? runtimeQ.dataUpdatedAt : null
  return <NowClock><section aria-label="Work history" className="flex w-full min-w-0 flex-col gap-3">
    {!unreadable && <p className="text-xs text-muted-foreground">Runtime updated: <time title={runtime?.generated_at}><Tick text={now => relative(runtime?.generated_at || '', now)} /></time></p>}
    {runtimeQ.isPending && !runtimeQ.isPaused && <p role="status">Loading agent runtime…</p>}
    {unreadable && <p role="alert"><Tick text={now => `${runtimeUnreadLine(readAt, openedAt.current, now)}${runtimeQ.error ? ` Error: ${runtimeQ.error.message}` : ' Offline.'}${runtime ? ' Below is what was read then.' : ''}`} /></p>}
    <ProgressTimeline runtime={runtime} now={Date.now()} />
  </section></NowClock>
}
