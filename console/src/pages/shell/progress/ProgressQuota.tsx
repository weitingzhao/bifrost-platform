import { DenseTag } from '@bifrost/ui'
import type { ProgressCodexUsage, ProgressCursorUsage, ProgressUsage } from '@/api/agentProgressBoard'
import type { ProgressPool } from '@/api/agentProgressExtra'
import { Tick } from '@/pages/shell/progress/progressClock'
import {
  barPercent,
  paceLine,
  poolResetsLine,
  poolsByVendor,
  usedLine,
  verdictChip,
  type QuotaTone,
} from '@/pages/shell/progress/quotaModel'

function cursorLine(cursor: ProgressCursorUsage): string {
  const parts = ['Cursor']
  if (cursor.runs != null) parts.push(`${cursor.runs} ${cursor.runs === 1 ? 'run' : 'runs'}`)
  if (cursor.min != null) parts.push(`${cursor.min} min`)
  if (cursor.in != null) parts.push(`in ${cursor.in}`)
  if (cursor.out != null) parts.push(`out ${cursor.out}`)
  if (cursor.cache != null) parts.push(`cache ${cursor.cache}`)
  return parts.join(' · ')
}

function codexLine(codex: ProgressCodexUsage): string {
  const parts = ['Codex']
  if (codex.runs != null) parts.push(`${codex.runs} ${codex.runs === 1 ? 'run' : 'runs'}`)
  if (codex.tokens != null) parts.push(`${codex.tokens} tokens`)
  return parts.join(' · ')
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
  const chip = verdictChip(pool)
  const pace = paceLine(pool)
  const label = pool.label?.trim() ?? ''
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <p className="m-0 break-words text-sm text-foreground">
        {label !== '' ? <span className="font-medium">{label}</span> : null}
        <Tick
          text={now => {
            const parts = [usedLine(pool.used), poolResetsLine(pool.resets_at, now)].filter(
              (part): part is string => part != null && part !== '',
            )
            if (parts.length === 0) return null
            const body = parts.join(' · ')
            return label !== '' ? ` · ${body}` : body
          }}
        />
      </p>
      <PoolBar pool={pool} />
      {pace != null ? <p className="m-0 break-words text-sm text-foreground">{pace}</p> : null}
      {chip != null ? (
        <DenseTag variant={toneVariant(chip.tone)} size="pill">
          {chip.text}
        </DenseTag>
      ) : null}
    </div>
  )
}

/** One vendor, then its pools, then that vendor's run total for today. */
export function ProgressQuota({ usage }: { usage: ProgressUsage }) {
  const groups = poolsByVendor(usage.pools ?? [])
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
    </section>
  )
}
