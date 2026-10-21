import { useNow } from '@/pages/shell/progress/progressClock'
import { lastReadWords } from './ownerModel'

/**
 * After a failed read: when it last answered, or that it has not since this page opened — the words every runtime
 * read uses when it fails (gap 3; review 488 B1). openedAt is the page's or card's own mount time, the same one its
 * other lines use (review 493 C), not when this line first showed. It ticks with the page's clock, or reads the time
 * itself without one.
 */
export function LastRead({ readAt, openedAt }: { readAt: number | null | undefined; openedAt: number }) {
  const now = useNow() || Date.now()
  return <>{lastReadWords(readAt ?? null, openedAt, now)}</>
}
