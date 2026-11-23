import { frameAlertsOf } from '@/lib/shell/consoleFrame'
import { useQuery } from '@tanstack/react-query'
import { fetchChecklistSignals } from '@/api/checklist'
import { fetchSelfHealth } from '@/api/core'
import { fetchTelemetryAlerts } from '@/api/telemetry'
import { useLiveInterval } from '@/domains/foundation/bus/liveRefresh'
import { normalizeViewerEnv } from '@/lib/control-room/fleetSnapshot'
import { shellViewerHealthy, systemVerdict, type SystemVerdict } from '@/lib/shell/shellStatusLine'

function errorText(error: unknown, fallback: string): string | null {
  if (error == null) return null
  return error instanceof Error ? error.message : fallback
}

/**
 * The system verdict shown in the header and at the top of Status. Query keys are
 * shared with the Status page panels so both read the same responses.
 */
/** The shell's self-health query; whoever else reads self-health shares it. */
export const shellSelfHealthQuery = {
  queryKey: ['shell', 'self-health'],
  queryFn: fetchSelfHealth,
  refetchInterval: 20_000,
  staleTime: 10_000,
}

export function useSystemVerdict() {
  const health = useQuery(shellSelfHealthQuery)
  const checklist = useQuery({
    queryKey: ['status', 'checklist-signals'],
    queryFn: fetchChecklistSignals,
    refetchInterval: 60_000,
    retry: false,
  })
  // alert.rule.fired refreshes it while the bus stream is up (bus step 4).
  const alertsInterval = useLiveInterval(30_000)
  const alerts = useQuery({
    queryKey: ['telemetry', 'alerts'],
    queryFn: fetchTelemetryAlerts,
    refetchInterval: alertsInterval,
    retry: false,
  })
  const verdict: SystemVerdict = systemVerdict({
    health: {
      data: health.data,
      loading: health.isLoading,
      error: errorText(health.error, 'Self-health request failed'),
    },
    checklist: {
      data: checklist.data,
      loading: checklist.isLoading,
      error: errorText(checklist.error, 'Checklist request failed'),
    },
    alerts: {
      data: alerts.data,
      loading: alerts.isLoading,
      error: errorText(alerts.error, 'Alerts request failed'),
    },
  })
  return {
    verdict,
    health,
    frameAlerts: frameAlertsOf(alerts.data, errorText(alerts.error, 'Alerts request failed')),
    refetch: async () => {
      await Promise.all([health.refetch(), checklist.refetch(), alerts.refetch()])
    },
  }
}

/** Shell header: the verdict sentence plus the viewer seat from self-health. */
export function useShellStatusLine() {
  const { verdict, health, frameAlerts, refetch } = useSystemVerdict()
  const loading = health.isLoading && health.data == null
  return {
    statusLine: verdict.sentence,
    statusTone: verdict.tone,
    viewerEnv: normalizeViewerEnv(health.data?.viewer_env),
    viewerEnvLoading: loading,
    healthy: shellViewerHealthy(health.data),
    frameAlerts,
    refetch,
  }
}
