/**
 * Network API contract — platform-api /api/v1/network/* (catalog-only).
 *
 * Live routes and UniFi probe: Ground Systems → Network;
 * actuation audit via GET /api/v1/audit. Executor scripts per spine D9.
 */

import { buildUnifiMcpServerLlmPack } from './unifiMcpServerCatalog'

export const NETWORK_API_CONTRACT_VERSION = '2026-08-05'
export const NETWORK_API_CONTRACT_SOURCE = 'console/src/lib/architecture/networkApiContractCatalog.ts'
export const NETWORK_API_CONTRACT_STATUS =
  'L0 LIVE + L1 APPLY — GET /api/v1/network/* (health/bandwidth/anomalies/sla) + POST firewall/apply + MCP proxy'

export const NETWORK_API_MCP_SERVER = {
  path: 'mcp/unifi/src/index.ts',
  version: '2026-08-05',
  status: 'implemented',
  note: '11 read + 1 write tool proxy platform-api /api/v1/network/* (incl. health/bandwidth/anomalies/sla)',
} as const

export const NETWORK_API_CLIENT_LIBRARY = {
  path: 'api/internal/network/unifi',
  version: '2026-07-03',
  status: 'implemented',
  note: 'Session v2 Login + legacy/v2 read methods; unit tests via httptest',
} as const

export type NetworkApiRouteDef = {
  method: 'GET' | 'POST' | 'PUT' | 'DELETE'
  route: string
  purpose: string
  authLevel: 'viewer' | 'operator' | 'admin'
  autonomy: 'L0' | 'L1' | 'L2'
  implemented: boolean
  executor: string
  mcpTool?: string
}

/** Planned REST surface — mirrors Constitution North Star + Agent Protocol playbooks. */
export const NETWORK_API_ROUTES: NetworkApiRouteDef[] = [
  {
    method: 'GET',
    route: '/api/v1/network/status',
    purpose: 'UCG reachability, controller version, IDS/IPS posture read-only (no toggle)',
    authLevel: 'viewer',
    autonomy: 'L0',
    implemented: true,
    executor: 'api/internal/network/unifi Client + network.Handler',
    mcpTool: 'get_network_status',
  },
  {
    method: 'GET',
    route: '/api/v1/network/zones',
    purpose: 'Bifrost zone inventory + VLAN binding vs networkUpgradeCatalog.ts FIREWALL_APPLIED',
    authLevel: 'viewer',
    autonomy: 'L0',
    implemented: true,
    executor: 'UniFi v2 zones API',
    mcpTool: 'get_network_zones',
  },
  {
    method: 'GET',
    route: '/api/v1/network/policies',
    purpose: 'ZBF policy list mapped to FIREWALL_RULES catalog rows',
    authLevel: 'viewer',
    autonomy: 'L0',
    implemented: true,
    executor: 'UniFi v2 firewall-policies API',
    mcpTool: 'get_network_policies',
  },
  {
    method: 'GET',
    route: '/api/v1/network/audit',
    purpose:
      'Firewall drift classification — POLICY_NOMINAL | POLICY_DRIFT | SESSION_PATH (parallel to verify_payload)',
    authLevel: 'viewer',
    autonomy: 'L0',
    implemented: true,
    executor: 'scripts/unifi_firewall_setup.py audit (wrapped)',
    mcpTool: 'audit_network_firewall',
  },
  {
    method: 'GET',
    route: '/api/v1/network/devices',
    purpose: 'UCG / switch / AP inventory, port stats summary',
    authLevel: 'viewer',
    autonomy: 'L0',
    implemented: true,
    executor: 'UniFi stat/device API',
    mcpTool: 'get_network_devices',
  },
  {
    method: 'GET',
    route: '/api/v1/network/clients',
    purpose: 'Client count per VLAN / SSID — Default VLAN empty check (Constitution success criteria)',
    authLevel: 'viewer',
    autonomy: 'L0',
    implemented: true,
    executor: 'UniFi stat/sta API',
    mcpTool: 'get_network_clients',
  },
  {
    method: 'GET',
    route: '/api/v1/network/health',
    purpose: 'UniFi stat/health + device online fraction (network-monitoring-ops Wave A)',
    authLevel: 'viewer',
    autonomy: 'L0',
    implemented: true,
    executor: 'unifi.Client.Health + projected devices',
    mcpTool: 'get_network_health',
  },
  {
    method: 'GET',
    route: '/api/v1/network/bandwidth',
    purpose: 'Device/client rx/tx bytes and rates (Wave B)',
    authLevel: 'viewer',
    autonomy: 'L0',
    implemented: true,
    executor: 'UniFi stat/device + stat/sta projected rates',
    mcpTool: 'get_network_bandwidth',
  },
  {
    method: 'GET',
    route: '/api/v1/network/anomalies',
    purpose: 'Rule-based anomaly alerts — device down, probe streak, optional client drop (Wave C)',
    authLevel: 'viewer',
    autonomy: 'L0',
    implemented: true,
    executor: 'api/internal/network Anomalies (config via env)',
    mcpTool: 'get_network_anomalies',
  },
  {
    method: 'GET',
    route: '/api/v1/network/sla',
    purpose: 'Thin SLA strip — probe success, device up fraction, predictive-lite tips (Wave D)',
    authLevel: 'viewer',
    autonomy: 'L0',
    implemented: true,
    executor: 'Health + Anomalies aggregate',
    mcpTool: 'get_network_sla',
  },
  {
    method: 'POST',
    route: '/api/v1/network/firewall/apply',
    purpose: 'L1 idempotent re-sync missing Bifrost ZBF policies (Agent Protocol POLICY_DRIFT remediation)',
    authLevel: 'operator',
    autonomy: 'L1',
    implemented: true,
    executor: 'scripts/unifi_firewall_setup.py apply — Session v2 per decision D9',
    mcpTool: 'apply_network_firewall',
  },
  {
    method: 'POST',
    route: '/api/v1/network/zones/restructure',
    purpose: 'Zone restructure — L2 Owner confirm; never bulk-delete all Bifrost zones via API',
    authLevel: 'admin',
    autonomy: 'L2',
    implemented: false,
    executor: 'Future UniFi v2 zone writer + audit trail',
  },
  {
    method: 'POST',
    route: '/api/v1/network/wlan',
    purpose: 'SSID CRUD — L2 Owner confirm; pre-create via unifi_wlan_precreate.py pattern',
    authLevel: 'admin',
    autonomy: 'L2',
    implemented: false,
    executor: 'scripts/unifi_wlan_precreate.py apply (future wrapper)',
  },
]

export const NETWORK_API_FORBIDDEN = [
  'Default Security Posture toggle (Allow All ↔ Block All) or disable IDS/IPS — not exposed to platform AI',
  'Bulk delete all Bifrost firewall zones / policies',
  'UniFi Integration API Key write on UCG 10.4.57 — use Session v2 per spine D9',
  'Manual UniFi UI changes — Owner physical hardware only; routine changes via platform-api + scripts',
] as const

/** Owner-deferred routes — documented contract only; Wave A Phase A3 explicitly out of agent scope. */
export const NETWORK_API_OWNER_DEFERRED = [
  {
    id: 'network-l2-zones-wlan',
    routes: 'POST /api/v1/network/zones/restructure · POST /api/v1/network/wlan',
    autonomy: 'L2',
    reason: 'Owner-confirmed physical UniFi changes — see blueprintCatalog.ts D6_APPENDIX_OWNER_DEFERRED',
  },
  {
    id: 'network-p2-ap-lifecycle',
    routes: 'AP adopt / restart / firmware (no platform-api route yet)',
    autonomy: 'P2',
    reason: 'Owner-deferred actuation phase slice — not implemented in Wave A',
  },
] as const

export const NETWORK_API_EXECUTOR_MODEL = {
  primary: 'scripts/unifi_firewall_setup.py audit | apply',
  sessionPath: 'bifrost-agent Super Admin local account — UniFi v2 API + CSRF (spine decision D9)',
  catalogAuthority: 'networkUpgradeCatalog.ts — FIREWALL_APPLIED + FIREWALL_RULES',
  auditTrail: 'POST actuation → GET /api/v1/audit (cluster/gitops L1); Trade satellite → POST /api/v1/audit/append (operator token)',
  spineStream: 'unifi-mcp-server — ①–④ complete (client, L0+MCP read, live probe, MCP write L1 apply)',
  clientLibrary: 'api/internal/network/unifi — ConfigFromEnv, Login, LegacyGet, V2Get, ListDevices/Clients/Zones/Policies',
  mcpServer: 'mcp/unifi — get_network_* + audit_network_firewall + apply_network_firewall → platform-api',
} as const

export type NetworkApiMcpToolDef = {
  tool: string
  route: string
  level: 'read' | 'routine' | 'confirm'
  implemented: boolean
}

/** Future unifi-mcp-server tools — map 1:1 to platform-api routes (decoupling principle). */
export const NETWORK_API_MCP_TOOLS: NetworkApiMcpToolDef[] = NETWORK_API_ROUTES.filter(r => r.mcpTool != null).map(
  r => ({
    tool: r.mcpTool!,
    route: r.route,
    level: r.autonomy === 'L0' ? 'read' : r.autonomy === 'L1' ? 'routine' : 'confirm',
    implemented: r.implemented,
  }),
)

export const NETWORK_API_RELATED_AUTHORITIES = [
  'Live UniFi probe + infra streams: Ground Systems → Network (Mission Control → Control Room summary)',
  'Actuation audit: GET /api/v1/audit (same pattern as cluster L1)',
  'Agent playbooks: Agent Protocol → POLICY_NOMINAL / POLICY_DRIFT / SESSION_PATH',
  'UniFi MCP stream: unifiMcpServerCatalog.ts · spine unifi-mcp-server',
  'Firewall catalog authority: networkUpgradeCatalog.ts — FIREWALL_APPLIED + FIREWALL_RULES',
]

/** Archived implementation status and MCP stream progress — live routes via platform-api + Control Room. */
export function buildNetworkApiHistoricalAppendix(): string {
  const lines: string[] = [
    '## Historical progress (archived — do not treat as live)',
    '',
    `Contract status snapshot: ${NETWORK_API_CONTRACT_STATUS}`,
    '',
    '### Routes (implementation snapshot)',
    ...NETWORK_API_ROUTES.map(
      r =>
        `- **${r.method} ${r.route}** [${r.autonomy}/${r.authLevel}] implemented=${r.implemented} — ${r.purpose}`,
    ),
    '',
    '### MCP tools (implementation snapshot)',
    ...NETWORK_API_MCP_TOOLS.map(
      t => `- ${t.tool} → ${t.route} (${t.level}) implemented=${t.implemented}`,
    ),
    '',
    buildUnifiMcpServerLlmPack(),
  ]
  return lines.join('\n')
}

export function buildNetworkApiContractLlmPack(): string {
  const lines: string[] = [
    '# Bifrost Ops — Network API Contract (platform-api /api/v1/network/*)',
    `Version: ${NETWORK_API_CONTRACT_VERSION}`,
    `Source: ${NETWORK_API_CONTRACT_SOURCE}`,
    'Live probe + route health: Control Room Network Health + platform-api — not this catalog.',
    '',
    '## Executor model',
    `- Primary: ${NETWORK_API_EXECUTOR_MODEL.primary}`,
    `- Session: ${NETWORK_API_EXECUTOR_MODEL.sessionPath}`,
    `- Catalog: ${NETWORK_API_EXECUTOR_MODEL.catalogAuthority}`,
    `- Audit: ${NETWORK_API_EXECUTOR_MODEL.auditTrail}`,
    `- MCP: ${NETWORK_API_EXECUTOR_MODEL.spineStream}`,
    `- Client library: ${NETWORK_API_EXECUTOR_MODEL.clientLibrary}`,
    `- MCP server: ${NETWORK_API_EXECUTOR_MODEL.mcpServer}`,
    '',
    '## Routes (contract)',
    ...NETWORK_API_ROUTES.map(
      r =>
        `- **${r.method} ${r.route}** [${r.autonomy}/${r.authLevel}] — ${r.purpose} · executor: ${r.executor}`,
    ),
    '',
    '## Forbidden (never exposed)',
    ...NETWORK_API_FORBIDDEN.map(f => `- ${f}`),
    '',
    '## Owner-deferred (Wave A Phase A3 — do not implement without Owner unlock)',
    ...NETWORK_API_OWNER_DEFERRED.map(
      d => `- **${d.id}** [${d.autonomy}] ${d.routes} — ${d.reason}`,
    ),
    '',
    '## Related authorities',
    ...NETWORK_API_RELATED_AUTHORITIES.map(a => `- ${a}`),
    '',
    buildNetworkApiHistoricalAppendix(),
  ]
  return lines.join('\n')
}
