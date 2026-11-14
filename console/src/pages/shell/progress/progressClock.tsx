import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'

/**
 * Duration texts tick each second. The rest of the board does not: only a
 * component that reads this clock repaints.
 */
const NowContext = createContext(0)

export function NowClock({ children }: { children: ReactNode }) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [])
  return <NowContext.Provider value={now}>{children}</NowContext.Provider>
}

export function useNow(): number {
  return useContext(NowContext)
}

export function Tick({ text, className }: { text: (nowMs: number) => string | null; className?: string }) {
  const now = useNow()
  const value = text(now)
  if (value == null || value === '') return null
  if (className == null) return <>{value}</>
  return <span className={className}>{value}</span>
}

/** A span in words: seconds under a minute, minutes under an hour, then hours and minutes. */
export function duration(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  return s < 60 ? `${s} s` : s < 3600 ? `${Math.floor(s / 60)} min` : `${Math.floor(s / 3600)} h ${Math.floor(s % 3600 / 60)} min`
}
/** How long ago an RFC 3339 time was, or that it is unknown. */
export function relative(at: string, now: number): string {
  const ms = Date.parse(at)
  return Number.isFinite(ms) ? `${duration((now - ms) / 1000)} ago` : 'Time unknown'
}
