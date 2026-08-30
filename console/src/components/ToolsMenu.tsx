import { useQuery } from '@tanstack/react-query'
import {
  Button,
  Popover,
  PopoverContent,
  PopoverTrigger,
  cn,
} from '@bifrost/ui'
import { ExternalLink, Wrench } from 'lucide-react'
import { fetchClusterObservability } from '@/api/cluster'
import {
  OPS_TOOLS,
  resolveOpsToolUrl,
  type OpsToolId,
} from '@/lib/architecture/opsToolRackCatalog'

const REFETCH_MS = 60_000

function toolHref(id: OpsToolId, grafanaLive: string | null | undefined): string {
  if (id === 'grafana') return resolveOpsToolUrl('grafana', grafanaLive)
  return resolveOpsToolUrl(id)
}

/** Header Tools popover — always-on discovery for external UIs (new tab, no iframe). */
export function ToolsMenu({ className }: { className?: string }) {
  const obs = useQuery({
    queryKey: ['cluster', 'observability', 'tool-rack'],
    queryFn: fetchClusterObservability,
    refetchInterval: REFETCH_MS,
    retry: 1,
    staleTime: REFETCH_MS,
  })
  const grafanaLive = obs.data?.grafana_url ?? null

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className={cn(
            'h-7 gap-1 px-2 text-dense-caption font-medium shadow-sm',
            className,
          )}
          title="External tools — Gitea, Grafana, Dagster"
          aria-label="Open Tools menu"
        >
          <Wrench size={14} aria-hidden />
          Tools
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80 p-2" sideOffset={6}>
        <p className="m-0 px-1.5 pb-1.5 text-dense-meta font-semibold uppercase tracking-wide text-muted-foreground">
          External tools
        </p>
        <ul className="m-0 flex list-none flex-col gap-0.5 p-0">
          {OPS_TOOLS.map(tool => {
            const href = toolHref(tool.id, grafanaLive)
            return (
              <li key={tool.id}>
                <a
                  href={href}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-start gap-2 rounded-md px-1.5 py-1.5 text-foreground no-underline hover:bg-secondary/80"
                >
                  <ExternalLink
                    size={14}
                    className="mt-0.5 shrink-0 text-muted-foreground"
                    aria-hidden
                  />
                  <span className="min-w-0 flex-1">
                    <span className="block text-dense-label font-medium">{tool.label}</span>
                    <span className="block text-dense-caption text-muted-foreground">
                      {tool.purpose}
                    </span>
                    <span className="mt-0.5 block truncate font-mono text-dense-micro text-muted-foreground/80">
                      {href}
                    </span>
                  </span>
                </a>
              </li>
            )
          })}
        </ul>
        <p className="m-0 border-t border-border/60 px-1.5 pt-1.5 text-dense-micro text-muted-foreground">
          Opens in a new tab — not embedded in Console.
        </p>
      </PopoverContent>
    </Popover>
  )
}
