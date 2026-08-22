import type { ReactNode } from 'react'
import { DenseTag, cn } from '@bifrost/ui'
import { useTickFlash } from '@/components/market-data/useTickFlash'

export function FlashValue({
  value,
  invert = false,
  className,
  children,
}: {
  value: number | string | null | undefined
  invert?: boolean
  className?: string
  children?: ReactNode
}) {
  const flash = useTickFlash(value)
  const tick =
    flash === 'up' ? (invert ? 'md-tick-down' : 'md-tick-up') : flash === 'down' ? (invert ? 'md-tick-up' : 'md-tick-down') : null
  return (
    <span className={cn('md-tick-value', tick, className)}>
      {children}
    </span>
  )
}

/** Label + capped meter + value — numbers sit next to the bar, not the far edge. */
export function CoverageBarRow({
  name,
  nameTitle,
  fillPct,
  toneClass,
  meterLabel,
  value,
  valueText,
  invert,
  suffix,
}: {
  name: ReactNode
  nameTitle?: string
  fillPct: number
  toneClass: string
  meterLabel?: string
  value?: number | string | null
  valueText: ReactNode
  invert?: boolean
  suffix?: ReactNode
}) {
  return (
    <div className="grid grid-cols-[minmax(0,6.75rem)_minmax(3.25rem,9.5rem)_auto] items-center gap-x-1.5">
      <span
        className="min-w-0 truncate font-mono text-[var(--text-dense-caption)]"
        title={nameTitle}
      >
        {name}
      </span>
      <Meter fillPct={fillPct} toneClass={toneClass} label={meterLabel} />
      <div className="flex min-w-0 items-baseline justify-end gap-1 whitespace-nowrap">
        <FlashValue
          value={value}
          invert={invert}
          className="font-mono text-[var(--text-dense-caption)] tabular-nums"
        >
          {valueText}
        </FlashValue>
        {suffix}
      </div>
    </div>
  )
}

export function Meter({
  fillPct,
  toneClass,
  label,
  className,
}: {
  fillPct: number
  toneClass: string
  label?: string
  className?: string
}) {
  return (
    <div
      className={cn(
        'relative h-1.5 min-w-0 flex-1 overflow-hidden rounded-sm bg-[var(--muted)]',
        className,
      )}
      title={label}
    >
      <div
        className={`h-full ${toneClass}`}
        style={{ width: `${Math.max(0, Math.min(100, fillPct))}%` }}
      />
    </div>
  )
}

export function StackedBar({
  readyPct,
  thinPct,
  blockedPct,
}: {
  readyPct: number
  thinPct: number
  blockedPct: number
}) {
  return (
    <div className="flex h-1.5 overflow-hidden rounded-sm bg-[var(--muted)]">
      <div className="h-full bg-[var(--color-success)]" style={{ width: `${readyPct}%` }} />
      <div className="h-full bg-[var(--color-warning)]" style={{ width: `${thinPct}%` }} />
      <div
        className="h-full bg-[var(--color-danger,var(--destructive))]"
        style={{ width: `${blockedPct}%` }}
      />
    </div>
  )
}

export function ScoreRing({
  ready,
  thin = 0,
  blocked = 0,
  unknown = 0,
  total,
  caption = 'ready',
}: {
  ready: number
  thin?: number
  blocked?: number
  unknown?: number
  total: number
  caption?: string
}) {
  const denom = total > 0 ? total : 1
  const r = (ready / denom) * 100
  const t = (thin / denom) * 100
  const b = (blocked / denom) * 100
  const u = (unknown / denom) * 100
  const background = `conic-gradient(
    var(--color-success) 0 ${r}%,
    var(--color-warning) ${r}% ${r + t}%,
    var(--color-danger, var(--destructive)) ${r + t}% ${r + t + b}%,
    var(--muted-foreground) ${r + t + b}% ${r + t + b + u}%,
    var(--muted) ${r + t + b + u}% 100%
  )`
  return (
    <div
      className="relative h-11 w-11 shrink-0 rounded-full"
      style={{ background }}
      role="img"
      aria-label={`${ready} of ${total} ${caption}`}
    >
      <div className="absolute inset-[3px] flex flex-col items-center justify-center rounded-full bg-[var(--card)]">
        <FlashValue
          value={ready}
          className="font-mono text-[var(--text-dense-caption)] font-semibold leading-none tabular-nums"
        >
          {ready}/{total}
        </FlashValue>
      </div>
    </div>
  )
}

export function DashCard({
  title,
  tag,
  tagVariant,
  value,
  rawValue,
  invertFlash,
  unit,
  caption,
  captionTitle,
  children,
  onClick,
}: {
  title: string
  tag?: string
  tagVariant?: 'success' | 'warning' | 'danger' | 'neutral'
  value: string
  rawValue?: number | string | null
  invertFlash?: boolean
  unit?: string
  caption?: string
  captionTitle?: string
  children?: ReactNode
  onClick?: () => void
}) {
  const Tag = onClick ? 'button' : 'div'
  return (
    <Tag
      type={onClick ? 'button' : undefined}
      className={cn(
        'flex min-w-0 flex-col gap-1 rounded-md border border-[var(--border)] bg-[var(--secondary)] px-2 py-1.5 text-left',
        onClick && 'cursor-pointer hover:border-[var(--foreground)]/20',
      )}
      onClick={onClick}
    >
      <div className="flex items-center justify-between gap-1.5">
        <span className="truncate text-[var(--text-dense-caption)] font-medium">{title}</span>
        {tag != null ? <DenseTag variant={tagVariant ?? 'neutral'}>{tag}</DenseTag> : null}
      </div>
      <div className="flex items-baseline justify-between gap-1.5">
        <FlashValue
          value={rawValue ?? value}
          invert={invertFlash}
          className="font-mono text-[var(--text-dense-body)] font-semibold leading-none tabular-nums text-[var(--foreground)]"
        >
          {value}
        </FlashValue>
        {unit != null ? (
          <span className="text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">{unit}</span>
        ) : null}
      </div>
      {children}
      {caption != null ? (
        <p
          className="m-0 line-clamp-2 break-words text-[var(--text-dense-micro)] leading-snug text-[var(--muted-foreground)]"
          title={captionTitle ?? caption}
        >
          {caption}
        </p>
      ) : null}
    </Tag>
  )
}
