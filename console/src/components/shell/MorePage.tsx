import { ExternalLink } from 'lucide-react'
import { DetailPageLinks } from '@/components/shell/DetailPageLinks'
import { ToolsMenu } from '@/components/ToolsMenu'
import type { FleetViewerEnv } from '@/lib/control-room/fleetSnapshot'
import {
  resolveTradeFrontendUrls,
  tradeFrontendEnvFromViewer,
  TRADE_FRONTEND_ENV_OPTIONS,
} from '@/lib/tradeFrontendUrls'

/**
 * More, on a phone or a tablet: what a desktop's side nav holds beyond the four entries — every
 * detail page, the Trade app in each environment (the side nav's footer) and the external tools.
 * Every target is 44 px.
 */
export function MorePage({ viewerEnv, viewerEnvLoading = false }: { viewerEnv: FleetViewerEnv; viewerEnvLoading?: boolean }) {
  const urls = resolveTradeFrontendUrls()
  const preferred = viewerEnvLoading ? null : tradeFrontendEnvFromViewer(viewerEnv)
  return (
    <div className="flex flex-col gap-6">
      <DetailPageLinks />
      <nav aria-label="Trade" className="flex flex-col gap-2">
        <h2 className="m-0 text-base font-semibold">Trade</h2>
        <ul className="m-0 grid list-none grid-cols-3 gap-1.5 p-0">
          {TRADE_FRONTEND_ENV_OPTIONS.map(opt => (
            <li key={opt.id}>
              <a
                href={urls[opt.id]}
                target="_blank"
                rel="noopener noreferrer"
                aria-label={`Open Trade · ${opt.label}${opt.id === preferred ? ' (this seat)' : ''}`}
                className="flex min-h-11 items-center justify-center gap-1.5 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 font-semibold text-foreground no-underline"
              >
                {opt.label}
                {opt.id === preferred && <span className="text-xs font-normal text-muted-foreground">this seat</span>}
                <ExternalLink className="size-3.5 shrink-0 opacity-70" aria-hidden />
              </a>
            </li>
          ))}
        </ul>
      </nav>
      <div className="flex min-h-11 items-center gap-3 text-sm text-muted-foreground">
        <ToolsMenu className="h-11 w-11" />
        External tools: Gitea, Grafana, Dagster
      </div>
    </div>
  )
}
