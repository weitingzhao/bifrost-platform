import { cn } from '@bifrost/ui'

/** LAN IP with the last octet (e.g. `.75`) emphasized — Server Console host chips. */
export function ConsoleHostIpLabel({
  ip,
  className,
  suffixClassName,
  compact = false,
  /** Omit shared LAN prefix — show only `.75` (full IP stays on title via parent). */
  octetOnly = false,
}: {
  ip: string
  className?: string
  suffixClassName?: string
  /** Smaller prefix for dense tables. */
  compact?: boolean
  octetOnly?: boolean
}) {
  const lastDot = ip.lastIndexOf('.')
  const baseSize = compact || octetOnly ? 'text-dense-caption' : 'text-dense-label'
  const prefixSize = compact ? 'text-dense-micro' : undefined

  if (lastDot === -1) {
    return (
      <span className={cn('font-mono tabular-nums', baseSize, className)} title={ip}>
        {ip}
      </span>
    )
  }

  const prefix = ip.slice(0, lastDot)
  const suffix = ip.slice(lastDot)

  if (octetOnly) {
    return (
      <span
        className={cn('font-mono tabular-nums font-semibold text-primary', baseSize, className, suffixClassName)}
        title={ip}
      >
        {suffix}
      </span>
    )
  }

  return (
    <span className={cn('font-mono tabular-nums', baseSize, className)} title={ip}>
      <span className={cn('text-muted-foreground', prefixSize)}>{prefix}</span>
      <span className={cn('font-semibold text-primary', suffixClassName)}>{suffix}</span>
    </span>
  )
}
