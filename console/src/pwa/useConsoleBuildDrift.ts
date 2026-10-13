import { useEffect, useState } from 'react'
import {
  CONSOLE_BUILD_CHECK_MS,
  buildsDiffer,
  moduleScriptSrc,
  ownModuleScriptSrc,
} from '@/pwa/consoleBuild'

/**
 * Compares this page's module script with a fresh index.html.
 * A failed check leaves the last conclusion alone, so being offline is not a new deploy.
 */
export function useConsoleBuildDrift(): boolean {
  const [stale, setStale] = useState(false)
  useEffect(() => {
    let cancelled = false
    const check = async () => {
      const own = ownModuleScriptSrc(document)
      if (own == null) return
      try {
        const response = await fetch('/index.html', { cache: 'no-store' })
        if (!response.ok) return
        const server = moduleScriptSrc(await response.text())
        if (server == null || cancelled) return
        setStale(buildsDiffer(own, server))
      } catch {
        // No answer: do not invent a newer build.
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
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [])
  return stale
}
