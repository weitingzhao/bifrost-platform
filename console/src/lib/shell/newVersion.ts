import { useEffect, useRef, useState } from 'react'
import { useConsoleBuildDrift } from '@/pwa/useConsoleBuildDrift'
import { moduleScriptSrc } from '@/pwa/consoleBuild'
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

/**
 * Whether the newer build can actually load: its index.html answers and names a module script, and that script
 * answers 200. While a rollout still splits requests between old and new replicas, the new page's script can 404 or
 * 502, and a reload then lands on a white page (2026-11-24, the Owner's phone could not open ops.bifrost.lan after a
 * deploy). Reload only when this is true; otherwise keep the page that works.
 */
export async function liveBuildReady(fetcher: typeof fetch = fetch): Promise<boolean> {
  try {
    const index = await fetcher('/index.html', { cache: 'no-store' })
    if (!index.ok) return false
    const src = moduleScriptSrc(await index.text())
    if (!src) return false
    const script = await fetcher(src, { cache: 'no-store' })
    return script.ok
  } catch {
    return false
  }
}
const IDLE_CHECK_MS = 30_000

/** Reload on its own only when a newer build is live and nobody has touched the page for `idleMs`. */
export function shouldAutoReload(stale: boolean, lastInputMs: number, nowMs: number, idleMs: number = IDLE_MS): boolean {
  return stale && nowMs - lastInputMs >= idleMs
}

/**
 * The shell's idle reload: mounted once. Idle counts from the last click, key, input, scroll or touch since the page
 * opened, not from when the newer build was found (箭10 2026-11-23): a page nobody has touched for ten minutes
 * reloads the moment a newer build is found; one touched a minute ago waits until it has sat five.
 */
export function useIdleReloadWhenStale(opts: { reload?: () => void; now?: () => number; idleMs?: number; ready?: () => Promise<boolean> } = {}): void {
  const stale = useConsoleBuildDrift()
  const { reload = () => window.location.reload(), now = Date.now, idleMs = IDLE_MS, ready = () => liveBuildReady() } = opts
  const lastInput = useRef<number>(now())
  useEffect(() => {
    const touch = () => { lastInput.current = now() }
    const events = ['pointerdown', 'keydown', 'input', 'wheel', 'touchstart'] as const
    events.forEach(e => window.addEventListener(e, touch, { passive: true }))
    return () => events.forEach(e => window.removeEventListener(e, touch))
    // now is stable for a page
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  useEffect(() => {
    if (!stale) return
    let checking = false
    const check = () => {
      if (checking || onDecisionPage() || !shouldAutoReload(true, lastInput.current, now(), idleMs)) return
      checking = true
      void ready().then(ok => { checking = false; if (ok) reload() })
    }
    check()
    const timer = window.setInterval(check, Math.min(IDLE_CHECK_MS, idleMs))
    return () => window.clearInterval(timer)
    // reload / now are stable for a page; re-arming only on drift keeps the check honest
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stale, idleMs])
}

/** The frame's notice while a newer build is live: "New version · Reload" (info: information, not a judgement). */
export function useNewVersionNotice(): { word: 'info' | 'warn'; text: string; action: { label: string; onActivate: () => void } } | undefined {
  const stale = useConsoleBuildDrift()
  const [notReady, setNotReady] = useState(false)
  if (!stale || onDecisionPage()) return undefined
  // A click reloads only when the newer build loads; otherwise the page stays and says so (liveBuildReady).
  const onActivate = () => { void liveBuildReady().then(ok => { if (ok) window.location.reload(); else setNotReady(true) }) }
  return notReady
    ? { word: 'warn', text: 'New version not ready', action: { label: 'Retry', onActivate } }
    : { word: 'info', text: 'New version', action: { label: 'Reload', onActivate } }
}
