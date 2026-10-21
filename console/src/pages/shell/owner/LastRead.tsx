import { useRef } from 'react'
import { useNow } from '@/pages/shell/progress/progressClock'
import { lastReadWords } from './ownerModel'

/**
 * After a failed read: when it last answered, or that it has not since this page opened — the words every runtime
 * read uses when it fails (gap 3; review 488 B1). It ticks with the page's clock, or reads the time itself without one.
 */
export function LastRead({ readAt }: { readAt: number | null | undefined }) {
  const openedAt = useRef(Date.now())
  const now = useNow() || Date.now()
  return <>{lastReadWords(readAt ?? null, openedAt.current, now)}</>
}
