import { useEffect, useState } from 'react'

/**
 * Wall-clock ticker for relative-time labels ("3s ago", elapsed, …).
 * Defaults to 1s; cleanup on unmount.
 */
export function useNowMs(intervalMs = 1000): number {
  const [nowMs, setNowMs] = useState(() => Date.now())
  useEffect(() => {
    const id = window.setInterval(() => setNowMs(Date.now()), intervalMs)
    return () => window.clearInterval(id)
  }, [intervalMs])
  return nowMs
}
