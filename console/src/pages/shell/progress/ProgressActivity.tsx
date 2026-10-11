import { type ReactNode } from 'react'
import { stepStateLabel, type ProgressFeedItem } from '@/api/agentProgress'
import { formatHourMinute } from '@/pages/shell/progress/progressModel'
import { useFolds } from '@/pages/shell/progress/progressFold'

function Collapsible({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  const folds = useFolds()
  const open = folds.isOpen(id, false)
  return (
    <section id={id} aria-label={title} className="w-full min-w-0">
      <details open={open} className="min-w-0 max-w-full">
        <summary
          className="flex min-h-11 cursor-pointer items-center text-sm font-medium text-foreground"
          onClick={event => {
            event.preventDefault()
            folds.toggle(id, false)
          }}
        >
          {title}
        </summary>
        {open ? <div className="mt-2 min-w-0">{children}</div> : null}
      </details>
    </section>
  )
}

/** The page's own words. A note is not a state, so that word is left off. */
function activityState(state: string | undefined): string {
  const raw = (state ?? '').trim()
  if (raw === '' || raw === 'note') return ''
  return stepStateLabel(raw, false)
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
    <Collapsible id="sec-activity" title="Activity">
      <ul aria-label="Activity" className="m-0 flex w-full min-w-0 list-none flex-col gap-1 p-0">
        {rows.map((item, index) => {
          const time = typeof item.at === 'number' ? formatHourMinute(item.at) : ''
          const text = [item.work, item.step, item.who, activityState(item.state), item.text]
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
    <Collapsible id="sec-done" title="Done today">
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
