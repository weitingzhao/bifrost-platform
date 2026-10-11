import { useEffect, useState, type ReactNode } from 'react'
import type { ProgressFeedItem } from '@/api/agentProgress'
import { formatHourMinute } from '@/pages/shell/progress/progressModel'

const NARROW = '(max-width: 360px)'

function useNarrow(): boolean {
  const [narrow, setNarrow] = useState(() => {
    if (typeof window.matchMedia !== 'function') return false
    return window.matchMedia(NARROW).matches
  })
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return
    const mq = window.matchMedia(NARROW)
    const apply = () => setNarrow(mq.matches)
    apply()
    mq.addEventListener('change', apply)
    return () => mq.removeEventListener('change', apply)
  }, [])
  return narrow
}

function Collapsible({ title, children }: { title: string; children: ReactNode }) {
  const narrow = useNarrow()
  return (
    <section aria-label={title} className="w-full min-w-0">
      <details open={!narrow} className="min-w-0 max-w-full">
        <summary className="cursor-pointer text-sm font-medium text-foreground">{title}</summary>
        <div className="mt-2 min-w-0">{children}</div>
      </details>
    </section>
  )
}

/** Newest first, at most 25. Empty feed is not drawn. */
export function ProgressActivity({ feed }: { feed: ProgressFeedItem[] | undefined }) {
  const rows = [...(feed ?? [])]
    .map((item, index) => ({ item, index }))
    .sort((a, b) => (b.item.at ?? 0) - (a.item.at ?? 0) || a.index - b.index)
    .slice(0, 25)
    .map(row => row.item)
  if (rows.length === 0) return null
  return (
    <Collapsible title="Activity">
      <ul aria-label="Activity" className="m-0 flex w-full min-w-0 list-none flex-col gap-1 p-0">
        {rows.map((item, index) => {
          const time = typeof item.at === 'number' ? formatHourMinute(item.at) : ''
          const text = [item.work, item.step, item.who, item.state, item.text]
            .map(part => (part ?? '').trim())
            .filter(part => part !== '')
            .join(' · ')
          return (
            <li key={index} className="break-words text-sm text-foreground">
              {time !== '' ? <time className="mr-2 text-muted-foreground">{time}</time> : null}
              <span>{text}</span>
            </li>
          )
        })}
      </ul>
    </Collapsible>
  )
}

/** Today's results. Empty or absent is not drawn. */
export function ProgressDoneToday({ recent }: { recent: string[] | undefined }) {
  if (recent == null || recent.length === 0) return null
  return (
    <Collapsible title="Done today">
      <ul aria-label="Done today" className="m-0 flex list-none flex-col gap-1 p-0">
        {recent.map((line, index) => (
          <li key={index} className="break-words text-sm text-foreground">
            {line}
          </li>
        ))}
      </ul>
    </Collapsible>
  )
}
