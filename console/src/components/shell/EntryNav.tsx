import { cn } from '@bifrost/ui'
import { ConsoleNavBadgeMark, type ConsoleNavBadge } from '@/components/shell/ConsoleNavSlotItem'
import type { EntryRouteId } from '@/lib/shell/consoleRoutes'
import { shellEntryItems } from '@/lib/shell/shellNavItems'
import { VERDICT_DOT_COLOR, type VerdictTone } from '@/lib/shell/shellStatusLine'

/** The short labels the tab bar and the rail use; the side nav and page titles use the full ones. */
const SHORT: Record<EntryRouteId, string> = { you: 'For you', chief: 'Chief', work: 'Work', system: 'System' }

/** System carries a lamp, never a second number, and only when the system verdict is not all clear. */
function lampTone(tone: VerdictTone): VerdictTone | null {
  return tone === 'degraded' || tone === 'failing' || tone === 'unknown' ? tone : null
}

export type EntryNavProps = {
  /** The entry lit now; null on More, which belongs to none. */
  active: EntryRouteId | null
  onSelect: (id: EntryRouteId) => void
  /** The one count of what is yours, on For you. */
  yoursBadge?: ConsoleNavBadge
  systemTone: VerdictTone
  systemLine: string
}

function EntryButtons({ active, onSelect, yoursBadge, systemTone, systemLine, rail }: EntryNavProps & { rail: boolean }) {
  const lamp = lampTone(systemTone)
  return shellEntryItems().map(item => {
    const id = item.id as EntryRouteId
    const on = id === active
    const Icon = item.icon
    return (
      <button
        key={id}
        type="button"
        data-entry={id}
        aria-current={on ? 'page' : undefined}
        aria-label={id === 'you' && yoursBadge ? `${item.label}, ${yoursBadge.title}` : item.label}
        onClick={() => onSelect(id)}
        className={cn(
          'relative flex flex-col items-center justify-center gap-1 rounded-xl border-0 bg-transparent text-[11px] font-semibold leading-tight',
          rail ? 'min-h-[60px] w-full' : 'min-h-[54px] min-w-0',
          on ? 'text-primary' : 'text-muted-foreground',
          on && rail && 'bg-[var(--control-fill)]',
        )}
      >
        {Icon != null && <Icon className="size-6" aria-hidden />}
        <span className="max-w-full truncate">{SHORT[id]}</span>
        {id === 'you' && yoursBadge != null && (
          <span className={cn('absolute', rail ? 'right-3 top-1.5' : 'left-[calc(50%+6px)] top-1')}>
            <ConsoleNavBadgeMark badge={yoursBadge} />
          </span>
        )}
        {id === 'system' && lamp != null && (
          <span
            data-system-lamp={lamp}
            title={systemLine}
            className={cn('absolute size-2 rounded-full', rail ? 'right-5 top-2.5' : 'left-[calc(50%+8px)] top-2')}
            style={{ backgroundColor: VERDICT_DOT_COLOR[lamp] }}
          />
        )}
      </button>
    )
  })
}

/** Phone and tablet held upright: the four entries at the bottom, under the thumb. */
export function EntryTabBar(props: EntryNavProps) {
  return (
    <nav
      aria-label="Main"
      data-entry-nav="tabs"
      className="fixed inset-x-0 bottom-0 z-30 grid grid-cols-4 gap-1 border-t border-[var(--table-rule)] bg-card px-2 pt-1.5 pb-[calc(0.375rem+env(safe-area-inset-bottom))]"
    >
      <EntryButtons {...props} rail={false} />
    </nav>
  )
}

/** Tablet held sideways: an 88 px icon rail with labels under the icons. */
export function EntryRail(props: EntryNavProps) {
  return (
    <nav
      aria-label="Main"
      data-entry-nav="rail"
      className="sticky top-0 flex h-svh w-[88px] shrink-0 flex-col gap-1.5 border-r border-[var(--table-rule)] bg-card px-2 py-3.5"
    >
      <EntryButtons {...props} rail />
    </nav>
  )
}
