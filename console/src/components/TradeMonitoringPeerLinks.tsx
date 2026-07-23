import { ExternalLink } from 'lucide-react'
import {
  cn,
  shellNavCollapsedIconButtonClass,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  useSidebar,
} from '@bifrost/ui'
import type { FleetViewerEnv } from '@/lib/control-room/fleetSnapshot'
import {
  resolveTradeFrontendUrls,
  tradeEnvChipClass,
  tradeFrontendEnvFromViewer,
  TRADE_FRONTEND_ENV_OPTIONS,
  type TradeFrontendEnvId,
} from '@/lib/tradeFrontendUrls'

export type TradeMonitoringPeerLinksProps = {
  viewerEnv: FleetViewerEnv
  viewerEnvLoading?: boolean
  className?: string
}

/**
 * Sidebar peer: Trade + Dev/Stg/Prod chips on one compact row.
 * Preferred env follows Fleet Viewer seat.
 */
export function TradeMonitoringPeerLinks({
  viewerEnv,
  viewerEnvLoading = false,
  className,
}: TradeMonitoringPeerLinksProps) {
  const { state } = useSidebar()
  const collapsed = state === 'collapsed'
  const urls = resolveTradeFrontendUrls()
  const preferred: TradeFrontendEnvId = viewerEnvLoading
    ? 'dev'
    : tradeFrontendEnvFromViewer(viewerEnv)
  const preferredHref = urls[preferred]
  const preferredLabel = preferred.toUpperCase()

  if (collapsed) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <a
            href={preferredHref}
            target="_blank"
            rel="noopener noreferrer"
            className={cn(shellNavCollapsedIconButtonClass(false), className)}
            aria-label={`Open Trade (${preferredLabel} · Viewer default)`}
          >
            <ExternalLink className="h-4 w-4 shrink-0" aria-hidden />
          </a>
        </TooltipTrigger>
        <TooltipContent side="right" className="text-xs font-medium">
          Trade · {preferredLabel}
        </TooltipContent>
      </Tooltip>
    )
  }

  return (
    <div
      className={cn(
        'mb-1.5 flex items-center gap-1.5 rounded-md border border-sidebar-border bg-sidebar-accent/40 px-2 py-1',
        className,
      )}
    >
      <a
        href={preferredHref}
        target="_blank"
        rel="noopener noreferrer"
        className="flex min-w-0 shrink items-center gap-1 text-[var(--text-dense-caption)] font-semibold text-sidebar-primary hover:underline"
        title={`Open Trade Monitoring (${preferredLabel} · Viewer default)`}
      >
        <span className="truncate">Trade</span>
        <ExternalLink className="h-3 w-3 shrink-0 opacity-70" aria-hidden />
      </a>
      <div
        className="flex shrink-0 items-center gap-0.5"
        role="group"
        aria-label="Trade Monitoring environments"
      >
        {TRADE_FRONTEND_ENV_OPTIONS.map(opt => {
          const href = urls[opt.id]
          const active = opt.id === preferred
          return (
            <a
              key={opt.id}
              href={href}
              target="_blank"
              rel="noopener noreferrer"
              className={cn(
                'rounded border px-1 py-px text-[10px] leading-tight no-underline transition-colors',
                tradeEnvChipClass(opt.id, active),
              )}
              title={
                active
                  ? `${opt.label} (Viewer default) → ${href}`
                  : `Open Trade · ${opt.label} → ${href}`
              }
            >
              {opt.label}
            </a>
          )
        })}
      </div>
    </div>
  )
}
