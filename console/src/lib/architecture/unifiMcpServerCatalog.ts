/** unifi-mcp-server spine stream — implementation catalog (Projection). */

export const UNIFI_MCP_SERVER_SOURCE = 'api/internal/network/unifi'
export const UNIFI_MCP_SERVER_CATALOG_VERSION = '2026-07-03'

export type UnifiMcpStreamPhaseId = 'UMS1' | 'UMS2' | 'UMS3' | 'UMS4'

export type UnifiMcpStreamPhase = {
  id: UnifiMcpStreamPhaseId
  spineStep: string
  title: string
  summary: string
  deliverable: string
  status: 'done' | 'in_progress' | 'pending'
}

/** Mirrors spine stream unifi-mcp-server (0/4 → progressive). */
export const UNIFI_MCP_SERVER_STREAM_PHASES: UnifiMcpStreamPhase[] = [
  {
    id: 'UMS1',
    spineStep: '①',
    title: 'UniFi REST API client library',
    summary:
      'Shared Go client (Session v2 + legacy v1 reads) — single library for platform-api /api/v1/network/* handlers.',
    deliverable: 'api/internal/network/unifi — Login, LegacyGet, V2Get, ListDevices/Clients/Zones/Policies',
    status: 'done',
  },
  {
    id: 'UMS2',
    spineStep: '②',
    title: 'MCP Server read tools',
    summary: 'unifi-mcp-server stdio tools wrap GET /api/v1/network/* (decoupling from platform-api).',
    deliverable: 'mcp/unifi/ + platform-api network handler L0 routes',
    status: 'done',
  },
  {
    id: 'UMS3',
    spineStep: '③',
    title: 'Ops Console Network Health live probe',
    summary: 'Control Room panel reads live UniFi status via platform-api (not catalog-only projection).',
    deliverable: 'NetworkHealthPanel liveProbe + GET /api/v1/network/status',
    status: 'pending',
  },
  {
    id: 'UMS4',
    spineStep: '④',
    title: 'MCP Server write tools',
    summary: 'L1/L2 actuation — firewall apply, zone restructure — audit trail via platform-api POST.',
    deliverable: 'MCP write tools + POST /api/v1/network/firewall/apply',
    status: 'pending',
  },
]

export const UNIFI_MCP_SERVER_IMPLEMENTATION_PROGRESS = {
  streamId: 'unifi-mcp-server',
  done: 2,
  total: 4,
  label: 'UniFi MCP Server — AI network management',
} as const

export function buildUnifiMcpServerLlmPack(): string {
  const lines = [
    '# UniFi MCP Server — implementation stream',
    `Version: ${UNIFI_MCP_SERVER_CATALOG_VERSION}`,
    `Client library: ${UNIFI_MCP_SERVER_SOURCE}`,
    `Progress: ${UNIFI_MCP_SERVER_IMPLEMENTATION_PROGRESS.done}/${UNIFI_MCP_SERVER_IMPLEMENTATION_PROGRESS.total}`,
    '',
    '## Phases',
  ]
  for (const p of UNIFI_MCP_SERVER_STREAM_PHASES) {
    lines.push(`- ${p.spineStep} ${p.id} ${p.title} [${p.status}] — ${p.deliverable}`)
  }
  return lines.join('\n')
}
