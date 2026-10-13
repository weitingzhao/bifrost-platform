import { useEffect, useState } from 'react'
import {
  CONSOLE_BUILD_CHECK_MS,
  buildsDiffer,
  moduleScriptSrc,
  ownModuleScriptSrc,
} from '@/pwa/consoleBuild'

type BuildState = { stale: boolean; checking: boolean; listeners: Set<(stale: boolean) => void> }
// The running script survives navigation, so confirmed drift survives it too.
const builds = new WeakMap<Element, BuildState>()
function currentBuild(): BuildState {
  const script = document.querySelector('script[type="module"][src]')
  if (script == null) return { stale: false, checking: false, listeners: new Set() }
  let state = builds.get(script)
  if (state == null) {
    state = { stale: false, checking: false, listeners: new Set() }
    builds.set(script, state)
  }
  return state
}

/**
 * Compares this page's module script with a fresh index.html.
 * A failed check leaves the last conclusion alone, so being offline is not a new deploy.
 */
export function useConsoleBuildDrift(): boolean {
  const [build] = useState(currentBuild)
  const [stale, setStale] = useState(build.stale)
  useEffect(() => {
    build.listeners.add(setStale)
    setStale(build.stale)
    let cancelled = false
    let stopCheck: (() => void) | undefined
    const check = async () => {
      const own = ownModuleScriptSrc(document)
      if (own == null || build.checking || build.stale) return
      build.checking = true
      const controller = new AbortController()
      let expired = false
      let expire!: () => void
      const deadline = new Promise<null>(resolve => { expire = () => resolve(null) })
      const stop = () => { expired = true; controller.abort(); expire() }
      stopCheck = stop
      const timeout = setTimeout(stop, 15_000)
      try {
        const read = async () => {
          const response = await fetch('/index.html', { cache: 'no-store', signal: controller.signal })
          return response.ok ? moduleScriptSrc(await response.text()) : null
        }
        const server = await Promise.race([read(), deadline])
        if (server != null && !expired && !cancelled && buildsDiffer(own, server)) {
          build.stale = true
          build.listeners.forEach(listener => listener(true))
        }
      } catch {
        // No answer: do not invent a newer build.
      } finally {
        clearTimeout(timeout)
        build.checking = false
        stopCheck = undefined
      }
    }
    void check()
    const timer = window.setInterval(() => {
      void check()
    }, CONSOLE_BUILD_CHECK_MS)
    const onVisible = () => {
      if (document.visibilityState === 'visible') void check()
    }
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      cancelled = true
      stopCheck?.()
      build.listeners.delete(setStale)
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [build])
  return stale
}
