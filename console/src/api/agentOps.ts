import type { AgentBridgeResponse, AgentDeployStatusResponse } from './agentTypes'
import { parseError } from './client'

export async function fetchAgentDeployStatus(): Promise<AgentDeployStatusResponse> {
  const r = await fetch('/api/v1/agent/deploy')
  if (!r.ok) throw await parseError('agent deploy status', r)
  return r.json() as Promise<AgentDeployStatusResponse>
}

export async function fetchAgentBridge(): Promise<AgentBridgeResponse> {
  const r = await fetch('/api/v1/agent/bridge')
  if (!r.ok) throw await parseError('agent bridge', r)
  return r.json() as Promise<AgentBridgeResponse>
}
