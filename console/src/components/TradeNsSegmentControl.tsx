/**
 * Trade NS SegmentControl with env-identity colors (SSOT: lib/envVisual.ts).
 * Same Dev / Stg / Prod options as Observability / Satellite Bus.
 */

import type { ReactNode } from 'react'
import {
  cn,
  segmentButtonClass,
  segmentGroupClass,
  type SegmentControlSize,
} from '@bifrost/ui'
import {
  isTradeEnvId,
  tradeEnvSegmentActiveClass,
  type TradeEnvId,
} from '@/lib/envVisual'

const DEFAULT_OPTIONS: { value: TradeEnvId; label: string }[] = [
  { value: 'dev', label: 'Dev' },
  { value: 'stg', label: 'Stg' },
  { value: 'prod', label: 'Prod' },
]

export function TradeNsSegmentControl({
  value,
  onChange,
  options = DEFAULT_OPTIONS,
  size = 'sm',
  className,
  ariaLabel = 'Trade namespace',
}: {
  value: TradeEnvId
  onChange: (env: TradeEnvId) => void
  /** Optional overrides (e.g. Satellite Bus lamp-enriched labels). */
  options?: { value: TradeEnvId; label: ReactNode; disabled?: boolean }[]
  size?: SegmentControlSize
  className?: string
  ariaLabel?: string
}) {
  return (
    <div
      className={cn(segmentGroupClass(size), className)}
      role="group"
      aria-label={ariaLabel}
    >
      {options.map(opt => {
        const active = value === opt.value
        const envClass =
          active && isTradeEnvId(opt.value) ? tradeEnvSegmentActiveClass(opt.value) : null
        return (
          <button
            key={opt.value}
            type="button"
            disabled={opt.disabled}
            onClick={() => onChange(opt.value)}
            className={cn(
              segmentButtonClass(false, size),
              active ? envClass : null,
            )}
            aria-pressed={active}
          >
            {opt.label}
          </button>
        )
      })}
    </div>
  )
}
