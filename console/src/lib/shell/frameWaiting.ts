import { useEffect, useMemo, useSyncExternalStore } from 'react'
import { NEEDS_YOU_HREF } from '@/lib/shell/consoleRoutes'
import { waitingKinds } from '@/pages/shell/needs-you/ticketKind'
import { useNeedsYou } from '@/pages/shell/needs-you/useNeedsYou'

/*
 * What waits on the Owner, laws and actions apart, in every view's frame (箭10 2026-11-24): "2 laws · 5 actions",
 * one click to Decisions & questions; both 0 draws nothing; a list that could not be read is "?". The shell works it
 * out once from the lists it already reads for the app badge (ticketKind.waitingKinds, the page's own numbers) and
 * publishes it; each frame only reads what was published, so a view mounts no readers of its own, and a page outside
 * the shell (a preview) has nothing published and draws nothing.
 */
export type FrameWaiting = { laws: number | null; actions: number | null; href: string; why?: string }

let published: FrameWaiting | undefined
const listeners = new Set<() => void>()

/** The shell's side: work the numbers out and publish them (mounted once, in ConsolePage). */
export function usePublishFrameWaiting(): void {
  const n = useNeedsYou()
  const value = useMemo<FrameWaiting>(() => {
    const k = waitingKinds({
      approvalsKnown: n.openCount.state === 'known',
      questionsKnown: !n.questionsLoading && !n.questionsUnreadable && !n.questionsStale,
      runtimeCount: n.runtimeCount.state === 'known' ? n.runtimeItems.length : null,
      decide: n.decide, terminal: n.terminal, check: n.check, openQuestions: n.openQuestions,
    })
    const why = [
      n.openCount.state === 'unknown' ? 'approvals not read' : '',
      n.questionsUnreadable || n.questionsStale ? `questions not read${n.questionsReason ? `: ${n.questionsReason}` : ''}` : '',
      n.runtimeCount.state === 'unknown' ? 'agent runtime not read' : '',
    ].filter(Boolean).join(' · ')
    return { ...k, href: NEEDS_YOU_HREF, ...(why ? { why } : {}) }
  }, [n.openCount.state, n.questionsLoading, n.questionsUnreadable, n.questionsStale, n.questionsReason, n.runtimeCount.state, n.runtimeItems.length, n.decide, n.terminal, n.check, n.openQuestions])
  useEffect(() => {
    publishFrameWaiting(value)
  }, [value])
}

/** Publish (also the test seam). undefined clears it. */
export function publishFrameWaiting(value: FrameWaiting | undefined): void {
  published = value
  listeners.forEach(l => l())
}

/** A frame's side: what the shell published, or undefined outside the shell. */
export function useFrameWaiting(): FrameWaiting | undefined {
  return useSyncExternalStore(
    l => { listeners.add(l); return () => listeners.delete(l) },
    () => published,
    () => undefined,
  )
}
