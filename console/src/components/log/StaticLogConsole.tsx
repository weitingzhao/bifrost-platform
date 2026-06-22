import { useMemo, useRef, useState } from 'react'
import { cn } from '@bifrost/ui'
import {
  filterStaticLogLines,
  parseStaticLogText,
  staticLogFilterLabel,
  STATIC_LOG_LEVEL_FILTER_OPTIONS,
  STATIC_LOG_LEVEL_LABEL,
  type StaticLogLevel,
  type StaticLogLevelFilter,
  type StaticLogLine,
} from '@/components/log/staticLogView'

const LEVEL_ROW_CLASS: Record<StaticLogLevel, string> = {
  ERROR: 'bg-[rgba(239,68,68,0.06)]',
  WARN: 'bg-[rgba(234,179,8,0.05)]',
  INFO: '',
  DEBUG: '',
  OTHER: '',
}

const LEVEL_BADGE_CLASS: Record<StaticLogLevel, string> = {
  ERROR: 'bg-[rgba(239,68,68,0.18)] text-[#fca5a5]',
  WARN: 'bg-[rgba(234,179,8,0.15)] text-[#fde047]',
  INFO: 'bg-[rgba(56,189,248,0.12)] text-[#7dd3fc]',
  DEBUG: 'bg-[rgba(161,161,170,0.12)] text-[#a1a1aa]',
  OTHER: 'bg-[rgba(161,161,170,0.08)] text-[#71717a]',
}

function LogLineRow({ line }: { line: StaticLogLine }) {
  return (
    <div
      className={cn(
        'flex min-w-0 items-baseline gap-2 px-3 py-[2px] font-mono text-[11px] leading-4 hover:bg-white/[0.03]',
        LEVEL_ROW_CLASS[line.level],
      )}
    >
      <span className="w-[62px] shrink-0 tabular-nums text-[#6b7280]">
        {line.ts || '—'}
      </span>
      <span
        className={cn(
          'w-[34px] shrink-0 rounded px-1 text-center text-[9px] font-semibold uppercase',
          LEVEL_BADGE_CLASS[line.level],
        )}
      >
        {STATIC_LOG_LEVEL_LABEL[line.level]}
      </span>
      <span className="min-w-0 flex-1 break-all text-[#e5e7eb]/90">{line.message}</span>
    </div>
  )
}

export interface StaticLogConsoleProps {
  logs: string | undefined
  loading?: boolean
  error?: string | null
  emptyMessage?: string
  defaultHeight?: number
  className?: string
}

export function StaticLogConsole({
  logs,
  loading = false,
  error = null,
  emptyMessage = 'No log lines returned',
  defaultHeight = 288,
  className,
}: StaticLogConsoleProps) {
  const [height, setHeight] = useState(defaultHeight)
  const [levelFilter, setLevelFilter] = useState<StaticLogLevelFilter>('ALERTS')
  const [search, setSearch] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)

  const parsed = useMemo(() => parseStaticLogText(logs ?? ''), [logs])
  const filtered = useMemo(
    () => filterStaticLogLines(parsed, levelFilter, search),
    [parsed, levelFilter, search],
  )

  return (
    <div
      className={cn(
        'min-w-0 overflow-hidden rounded-md border border-[var(--border)] bg-[#0a0c0f]',
        className,
      )}
    >
      <div className="flex flex-wrap items-center gap-2 border-b border-[var(--border)] bg-[var(--color-surface-elevated)] px-3 py-1.5">
        <div className="flex flex-wrap items-center gap-0.5">
          {STATIC_LOG_LEVEL_FILTER_OPTIONS.map(lv => (
            <button
              key={lv}
              type="button"
              onClick={() => setLevelFilter(lv)}
              className={cn(
                'rounded px-1.5 py-0.5 text-[10px] font-semibold transition-colors',
                levelFilter === lv
                  ? 'bg-[var(--foreground)]/10 text-[var(--foreground)]'
                  : 'text-[var(--muted-foreground)] hover:text-[var(--foreground)]',
              )}
            >
              {staticLogFilterLabel(lv)}
            </button>
          ))}
        </div>
        <input
          value={search}
          onChange={e => setSearch(e.target.value)}
          placeholder="Filter…"
          className="h-6 w-28 rounded border border-[var(--border)] bg-[var(--background)] px-2 text-[10px] outline-none focus:ring-1 focus:ring-[var(--ring)]"
        />
        <span className="ml-auto text-[10px] text-[var(--muted-foreground)]">
          {loading ? 'Loading…' : `${filtered.length} / ${parsed.length} lines`}
        </span>
      </div>

      <div
        ref={scrollRef}
        style={{ height }}
        className="overflow-x-hidden overflow-y-auto"
      >
        {loading ? (
          <p className="p-4 text-center text-[11px] text-[#9ca3af]">Loading logs…</p>
        ) : error != null ? (
          <p className="p-4 text-[11px] text-[#fca5a5]">{error}</p>
        ) : filtered.length === 0 ? (
          <p className="p-4 text-center text-[11px] text-[#9ca3af]">
            {parsed.length === 0 ? emptyMessage : 'No lines match filters'}
          </p>
        ) : (
          filtered.map(line => <LogLineRow key={line.id} line={line} />)
        )}
      </div>

      <div
        role="separator"
        aria-label="Resize log console height"
        className="h-2 cursor-row-resize border-t border-[var(--border)] bg-[var(--color-surface-elevated)] hover:bg-[var(--muted)]/40"
        onMouseDown={e => {
          e.preventDefault()
          const startY = e.clientY
          const startH = height
          function onMove(ev: MouseEvent) {
            setHeight(Math.max(160, Math.min(520, startH + (ev.clientY - startY))))
          }
          function onUp() {
            window.removeEventListener('mousemove', onMove)
            window.removeEventListener('mouseup', onUp)
          }
          window.addEventListener('mousemove', onMove)
          window.addEventListener('mouseup', onUp)
        }}
      />
    </div>
  )
}
