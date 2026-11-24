import { useEffect } from 'react'
import { useConsoleBuildDrift } from '@/pwa/useConsoleBuildDrift'
import { resolveConsoleHash } from '@/lib/shell/consoleRoutes'

/*
 * A newer build is live (箭10 2026-11-23): the Owner's tablet keeps the Console open, and a deploy he cannot see is a
 * change he cannot perceive. Detection is the existing build drift (pwa/useConsoleBuildDrift: this page's module script
 * against a fresh index.html). Here: the frame's notice for every view in the shell, and an idle page reloading itself.
 * A page in use (a click, a key, a scroll in the last five minutes) is never reloaded under the hand. The decision page
 * keeps its own banner and its own rule (no reload while a decision is in flight, and a typed reason must survive): no
 * frame notice and no idle reload there.
 */

/** The decision page, where reloading is the page's own call. */
export function onDecisionPage(hash: string = window.location.hash): boolean {
  return resolveConsoleHash(hash.replace(/^#/, '')).location.kind === 'approval'
}

export const IDLE_MS = 5 * 60_000
const IDLE_CHECK_MS = 30_000

/** Reload on its own only when a newer build is live and nobody has touched the page for `idleMs`. */
export function shouldAutoReload(stale: boolean, lastInputMs: number, nowMs: number, idleMs: number = IDLE_MS): boolean {
  return stale && nowMs - lastInputMs >= idleMs
}

/** The shell's idle reload: mounted once; reloads when the build drifted and the page sat idle. */
export function useIdleReloadWhenStale(opts: { reload?: () => void; now?: () => number; idleMs?: number } = {}): void {
  const stale = useConsoleBuildDrift()
  const { reload = () => window.location.reload(), now = Date.now, idleMs = IDLE_MS } = opts
  useEffect(() => {
    if (!stale) return
    let lastInput = now()
    const touch = () => { lastInput = now() }
    const events = ['pointerdown', 'keydown', 'input', 'wheel', 'touchstart'] as const
    events.forEach(e => window.addEventListener(e, touch, { passive: true }))
    const timer = window.setInterval(() => {
      if (!onDecisionPage() && shouldAutoReload(true, lastInput, now(), idleMs)) reload()
    }, Math.min(IDLE_CHECK_MS, idleMs))
    return () => {
      window.clearInterval(timer)
      events.forEach(e => window.removeEventListener(e, touch))
    }
    // reload / now are stable for a page; re-arming only on drift keeps the idle clock honest
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stale, idleMs])
}

/** The frame's notice while a newer build is live: "New version · Reload" (info: information, not a judgement). */
export function useNewVersionNotice(): { word: 'info'; text: string; action: { label: string; onActivate: () => void } } | undefined {
  const stale = useConsoleBuildDrift()
  return stale && !onDecisionPage() ? { word: 'info', text: 'New version', action: { label: 'Reload', onActivate: () => window.location.reload() } } : undefined
}
