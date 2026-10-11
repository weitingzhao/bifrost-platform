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
