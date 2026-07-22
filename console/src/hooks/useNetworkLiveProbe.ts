import { useQuery } from '@tanstack/react-query'
import { fetchNetworkAudit, fetchNetworkStatus } from '@/api/network'
import type { NetworkAuditResponse, NetworkStatusResponse } from '@/api/networkTypes'

export type NetworkLiveProbeState = {
  status: NetworkStatusResponse | undefined
  audit: NetworkAuditResponse | undefined
  isLoading: boolean
  isConfigured: boolean
  probeReach: 'ok' | 'degraded' | 'fail' | 'unknown'
  summary: string
}

function probeReach(
  status: NetworkStatusResponse | undefined,
  audit: NetworkAuditResponse | undefined,
  statusError: boolean,
): NetworkLiveProbeState['probeReach'] {
  if (status == null && statusError) return 'unknown'
  if (status?.error != null && status.error !== '') {
    if (status.hint != null) return 'unknown'
    return 'fail'
  }
  if (status?.reachable !== true) return 'fail'
  if (audit?.classification === 'POLICY_NOMINAL') return 'ok'
  if (audit?.classification === 'POLICY_DRIFT') return 'degraded'
  return 'degraded'
}

function buildSummary(
  status: NetworkStatusResponse | undefined,
  audit: NetworkAuditResponse | undefined,
): string {
  if (status?.error != null && status.error !== '') {
    return status.hint ?? status.error
  }
  const version = status?.controller_version?.trim() || 'unknown'
  const host = status?.host ?? 'UCG'
  const classification = audit?.classification ?? '…'
  return `${host} · Network ${version} · ${classification} · ${status?.session_path ?? 'SESSION_v2'}`
}

export function useNetworkLiveProbe(refetchIntervalMs = 30_000): NetworkLiveProbeState {
  const statusQ = useQuery({
    queryKey: ['network', 'live-probe', 'status'],
    queryFn: fetchNetworkStatus,
    refetchInterval: refetchIntervalMs,
    retry: 1,
  })
  const auditQ = useQuery({
    queryKey: ['network', 'live-probe', 'audit'],
    queryFn: fetchNetworkAudit,
    refetchInterval: refetchIntervalMs,
    retry: 1,
    enabled: statusQ.data?.reachable === true,
  })

  const status = statusQ.data
  const audit = auditQ.data
  const isLoading = statusQ.isLoading || (statusQ.data?.reachable === true && auditQ.isLoading)
  const isConfigured = status?.error == null || status.reachable === true

  return {
    status,
    audit,
    isLoading,
    isConfigured,
    probeReach: probeReach(status, audit, statusQ.isError),
    summary: buildSummary(status, audit),
  }
}
