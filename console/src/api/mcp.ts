import type { McpStatusResponse, McpToolsResponse } from './agentTypes'

export async function fetchMcpTools(): Promise<McpToolsResponse> {
  const r = await fetch('/api/v1/mcp/tools')
  if (!r.ok) throw new Error(`mcp tools: HTTP ${r.status}`)
  return r.json() as Promise<McpToolsResponse>
}

export async function fetchMcpStatus(): Promise<McpStatusResponse> {
  const r = await fetch('/api/v1/mcp/status')
  if (!r.ok) throw new Error(`mcp status: HTTP ${r.status}`)
  return r.json() as Promise<McpStatusResponse>
}

