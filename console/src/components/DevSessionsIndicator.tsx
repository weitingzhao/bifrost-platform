import { useQuery } from '@tanstack/react-query'
import { Button, StatusLamp, cn } from '@bifrost/ui'
import { Terminal } from 'lucide-react'
import { fetchDevSessions } from '@/api/devSession'

type Lamp = 'ok' | 'fail' | 'degraded' | 'unknown'

function sessionsLamp(running: number, total: number, errored: boolean): Lamp {
  if (errored || total === 0) return 'unknown'
  if (running === total) return 'ok'
  if (running === 0) return 'fail'
  return 'degraded'
}

/**
 * Shell top-bar host-runtime entry — StatusLamp + N/M deep-link to Dev Sessions.
 * Framework-level affordance; page logic stays on Engineer › Dev Sessions.
 */
export function DevSessionsIndicator({
  onOpen,
}: {
  onOpen: () => void
}) {
  const { data, isError, isLoading } = useQuery({
    queryKey: ['dev-sessions', 'shell'],
    queryFn: fetchDevSessions,
    refetchInterval: 15_000,
    retry: 1,
  })

  const sessions = data ?? []
  const total = sessions.length
  const running = sessions.filter(s => s.status === 'running').length
  const lamp = isLoading
    ? 'unknown'
    : sessionsLamp(running, total, isError)

  const label =
    isLoading ? '…' : isError || total === 0 ? '—' : `${running}/${total}`

  const title = isError
    ? 'Dev Sessions unreachable — open page'
    : isLoading
      ? 'Loading Dev Sessions…'
      : `${running}/${total} local host sessions running — open Dev Sessions`

  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      className={cn(
        'h-7 shrink-0 gap-1.5 px-2 shadow-sm',
        lamp === 'ok'
          ? 'border-border bg-secondary/50'
          : lamp === 'fail'
            ? 'border-[color-mix(in_oklab,var(--color-danger,#ef4444)_45%,var(--border))] bg-[color-mix(in_oklab,var(--color-danger,#ef4444)_10%,var(--card))]'
            : lamp === 'degraded'
              ? 'border-[color-mix(in_oklab,var(--color-warning,#f59e0b)_45%,var(--border))] bg-[color-mix(in_oklab,var(--color-warning,#f59e0b)_10%,var(--card))]'
              : 'border-border bg-secondary/50',
      )}
      onClick={onOpen}
      title={title}
      aria-label={title}
    >
      <StatusLamp value={lamp} kind="reach" />
      <Terminal size={12} className="text-muted-foreground" aria-hidden />
      <span className="font-mono text-[var(--text-dense-caption)] tabular-nums text-foreground">
        {label}
      </span>
    </Button>
  )
}
