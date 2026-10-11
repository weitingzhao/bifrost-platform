import { useState } from 'react'
import { DenseTag } from '@bifrost/ui'
import type { ProgressCodexUsage, ProgressCursorUsage, ProgressUsage } from '@/api/agentProgressBoard'
import type { ProgressPool } from '@/api/agentProgressExtra'
import { useNow } from '@/pages/shell/progress/progressClock'
import {
  barPercent,
  compactCount,
  paceLine,
  poolRead,
  poolResetsLine,
  poolsByVendor,
  usedIsDisplayable,
  usedLine,
  verdictChip,
  type QuotaTone,
} from '@/pages/shell/progress/quotaModel'

const POOL_CAP = 12

function finitePhrase(value: number | undefined): number | null {
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0) return null
  return Math.round(value)
}

function runsPhrase(runs: number | undefined): string | null {
  const rounded = finitePhrase(runs)
  if (rounded == null) return null
  return `${compactCount(rounded)} ${rounded === 1 ? 'run' : 'runs'}`
}

function minutePhrase(min: number | undefined): string | null {
  const rounded = finitePhrase(min)
  if (rounded == null) return null
  return `${compactCount(rounded)} min`
}

function amountPhrase(value: number | undefined, unit: string): string | null {
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0) return null
  return `${compactCount(value)} ${unit}`
}

function cursorLine(cursor: ProgressCursorUsage): string {
  const parts = ['Cursor', runsPhrase(cursor.runs), minutePhrase(cursor.min), amountPhrase(cursor.in, 'in'), amountPhrase(cursor.out, 'out'), amountPhrase(cursor.cache, 'cached')]
  return parts.filter((part): part is string => part != null && part !== '').join(' · ')
}

function codexLine(codex: ProgressCodexUsage): string {
  const parts = ['Codex', runsPhrase(codex.runs), amountPhrase(codex.tokens, 'tokens')]
  return parts.filter((part): part is string => part != null && part !== '').join(' · ')
}

function vendorTotal(vendor: string, usage: ProgressUsage): string | null {
  const key = vendor.trim().toLowerCase()
  if (key === 'cursor' && usage.cursor != null) return cursorLine(usage.cursor)
  if (key === 'codex' && usage.codex != null) return codexLine(usage.codex)
  return null
}

function toneVariant(tone: QuotaTone): 'success' | 'warning' | 'neutral' {
  if (tone === 'positive') return 'success'
  if (tone === 'warning') return 'warning'
  return 'neutral'
}

function PoolBar({ pool }: { pool: ProgressPool }) {
  if (!usedIsDisplayable(pool.used)) return null
  const used = barPercent(pool.used)
  const elapsed = barPercent(pool.elapsed_pct)
  if (used == null && elapsed == null) return null
  return (
    <div className="relative h-1.5 w-full max-w-full overflow-hidden rounded bg-foreground/10">
      {used != null ? (
        <div className="absolute inset-y-0 left-0 bg-foreground/70" style={{ width: `${used}%` }} data-quota-fill={used} />
      ) : null}
      {elapsed != null ? (
        <div
          data-window-tick={elapsed}
          className="absolute inset-y-0 w-0.5 bg-foreground"
          style={{ left: elapsed >= 100 ? 'calc(100% - 2px)' : `${elapsed}%` }}
        />
      ) : null}
    </div>
  )
}

function PoolRow({ pool }: { pool: ProgressPool }) {
  const now = useNow()
  const read = poolRead(pool, now)
  const label = pool.label?.trim() ?? ''
  const used = read === 'unknown' ? null : usedLine(pool.used)
  const reset = read === 'unknown' ? null : poolResetsLine(pool.resets_at, now)
  const pace = read === 'ok' ? paceLine(pool) : null
  const chip = read === 'unknown' ? { text: 'Unknown', tone: 'neutral' as const } : read === 'ok' ? verdictChip(pool) : null
  const note = read === 'ok' && pool.verdict === 'exhausted' ? (pool.note?.trim() ?? '') : ''
  const bits = [used, reset].filter((part): part is string => part != null && part !== '')
  const body = bits.join(' · ')
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <p className="m-0 break-words text-sm text-foreground">
        {label !== '' ? <span className="font-medium">{label}</span> : null}
        {body !== '' ? <span>{label !== '' ? ` · ${body}` : body}</span> : null}
      </p>
      {read === 'unknown' ? null : <PoolBar pool={pool} />}
      {pace != null ? <p className="m-0 break-words text-sm text-foreground">{pace}</p> : null}
      {chip != null ? (
        <DenseTag variant={toneVariant(chip.tone)} size="pill">
          {chip.text}
        </DenseTag>
      ) : null}
      {note !== '' ? <p className="m-0 break-words text-sm text-foreground">{note}</p> : null}
    </div>
  )
}

/** One vendor, then its pools, then that vendor's run total for today. */
export function ProgressQuota({ usage }: { usage: ProgressUsage }) {
  const [expanded, setExpanded] = useState(false)
  const all = usage.pools ?? []
  const hidden = Math.max(0, all.length - POOL_CAP)
  const shown = expanded || hidden === 0 ? all : all.slice(0, POOL_CAP)
  const groups = poolsByVendor(shown)
  return (
    <section aria-label="Quota" className="flex w-full min-w-0 flex-col gap-2">
      <h3 className="m-0 text-sm font-medium text-foreground">Quota</h3>
      <p className="m-0 break-words text-sm text-muted-foreground">
        Left of the tick: room to spare. Right of it: being used faster than the clock.
      </p>
      {groups.map((group, index) => {
        const total = vendorTotal(group.vendor, usage)
        return (
          <div key={`${group.vendor}-${index}`} className="flex min-w-0 flex-col gap-2">
            {group.vendor !== '' ? <p className="m-0 text-sm font-medium text-foreground">{group.vendor}</p> : null}
            {group.pools.map((pool, poolIndex) => (
              <PoolRow key={`${pool.id ?? pool.label ?? 'pool'}-${poolIndex}`} pool={pool} />
            ))}
            {total != null ? <p className="m-0 break-words text-sm text-muted-foreground">{total}</p> : null}
          </div>
        )
      })}
      {hidden > 0 && !expanded ? (
        <button type="button" className="w-fit text-sm text-foreground underline" onClick={() => setExpanded(true)}>
          {`Show ${hidden} more`}
        </button>
      ) : null}
    </section>
  )
}
