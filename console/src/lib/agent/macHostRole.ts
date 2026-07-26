import type { AgentBridgeResponse, RunnerStatus } from '@/api/agentTypes'

export type MacAgentHostRole = 'primary' | 'standby'

/**
 * Extract host/IP from a runner URL (`http://192.168.10.50:8781`)
 * or SSH remote (`vision@192.168.10.50`).
 */
export function hostKeyFromEndpoint(urlOrRemote: string): string | null {
  const raw = urlOrRemote.trim()
  if (raw === '') return null

  // SSH remote: user@host[:port] (no scheme)
  if (raw.includes('@') && !raw.includes('://')) {
    const hostPart = raw.slice(raw.indexOf('@') + 1).trim()
    const host = hostPart.split(':')[0]?.trim()
    return host !== '' ? host : null
  }

  try {
    const withScheme = raw.includes('://') ? raw : `http://${raw}`
    const hostname = new URL(withScheme).hostname.trim()
    return hostname !== '' ? hostname : null
  } catch {
    const bare = raw.match(/^(\d{1,3}(?:\.\d{1,3}){3})\b/)
    return bare?.[1] ?? null
  }
}

export function bridgeRunners(bridge: AgentBridgeResponse | null | undefined): RunnerStatus[] {
  if (bridge == null) return []
  if (bridge.runners != null && bridge.runners.length > 0) return bridge.runners
  return [bridge.remediation_runner]
}

/**
 * Map host IP → primary|standby from bridge runners only.
 * Non-runner Macs stay unlabeled (W1-P3 — avoid mis-tagging).
 */
export function buildMacAgentRoleByHost(
  bridge: AgentBridgeResponse | null | undefined,
): Record<string, MacAgentHostRole> {
  const map: Record<string, MacAgentHostRole> = {}
  for (const r of bridgeRunners(bridge)) {
    const role: MacAgentHostRole | null =
      r.role === 'standby' ? 'standby' : r.role === 'primary' ? 'primary' : null
    if (role == null) continue
    const key = hostKeyFromEndpoint(r.url)
    if (key != null) map[key] = role
  }
  return map
}

export function resolveMacAgentRole(
  hostIp: string,
  roleByHost: Record<string, MacAgentHostRole> | undefined,
): MacAgentHostRole | undefined {
  if (roleByHost == null) return undefined
  return roleByHost[hostIp]
}

export function macAgentRoleLabel(role: MacAgentHostRole): string {
  return role === 'primary' ? 'Primary' : 'Standby'
}

/** Match deploy target row → bridge runner (role first, then host IP). */
export function findRunnerForDeployTarget(
  bridge: AgentBridgeResponse | null | undefined,
  target: { role: string; remote: string },
): RunnerStatus | undefined {
  const runners = bridgeRunners(bridge)
  const byRole = runners.find(r => r.role === target.role)
  if (byRole != null) return byRole
  const remoteKey = hostKeyFromEndpoint(target.remote)
  if (remoteKey == null) return undefined
  return runners.find(r => hostKeyFromEndpoint(r.url) === remoteKey)
}

/** Map runner status string → StatusLamp reach value. */
export function runnerStatusReach(
  status: string | undefined,
): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (status === 'ok') return 'ok'
  if (status === 'unavailable') return 'fail'
  if (status == null || status === '' || status === 'not_configured') return 'unknown'
  return 'degraded'
}
