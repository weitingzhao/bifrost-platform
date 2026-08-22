import { useEffect, useRef, useState } from 'react'
import { tickRank } from '@/components/market-data/overviewDashModel'

export type TickFlash = 'up' | 'down' | null

/** Flash direction when a live metric changes. First paint is silent. */
export function useTickFlash(value: number | string | null | undefined): TickFlash {
  const prev = useRef<typeof value>(undefined)
  const primed = useRef(false)
  const [flash, setFlash] = useState<TickFlash>(null)

  useEffect(() => {
    if (!primed.current) {
      primed.current = true
      prev.current = value
      return
    }
    const a = tickRank(prev.current)
    const b = tickRank(value)
    prev.current = value
    if (a == null || b == null || a === b) return
    setFlash(b > a ? 'up' : 'down')
    const t = window.setTimeout(() => setFlash(null), 1100)
    return () => window.clearTimeout(t)
  }, [value])

  return flash
}
