#!/usr/bin/env node
/**
 * Bifrost UniFi MCP server (UMS2 + UMS4).
 * Read tools proxy GET /api/v1/network/* (L0); write tools proxy POST (L1).
 */
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js'
import { z } from 'zod'
import { jsonResult, platformGet, platformPost } from './platformClient.js'

const SERVER_NAME = 'mcp-server-unifi'
const SERVER_VERSION = '0.1.0'

const server = new McpServer({ name: SERVER_NAME, version: SERVER_VERSION })

server.tool('unifi_mcp_health', 'UniFi MCP server health + platform-api URL', {}, async () =>
  jsonResult({
    ok: true,
    server: SERVER_NAME,
    version: SERVER_VERSION,
    platform_api_url: process.env.PLATFORM_API_URL ?? 'http://127.0.0.1:8780',
    autonomy: 'L0',
  }),
)

server.tool(
  'get_network_status',
  'UCG reachability, controller version, Session v2 auth posture (L0)',
  {},
  async () => jsonResult(await platformGet('/api/v1/network/status')),
)

server.tool(
  'get_network_zones',
  'Bifrost firewall zone inventory + VLAN binding (L0)',
  {},
  async () => jsonResult(await platformGet('/api/v1/network/zones')),
)

server.tool(
  'get_network_policies',
  'ZBF policy list — Bifrost | policies highlighted (L0)',
  {},
  async () => jsonResult(await platformGet('/api/v1/network/policies')),
)

server.tool(
  'audit_network_firewall',
  'Firewall drift classification — POLICY_NOMINAL | POLICY_DRIFT | SESSION_PATH (L0)',
  {},
  async () => jsonResult(await platformGet('/api/v1/network/audit')),
)

server.tool(
  'get_network_devices',
  'UCG / switch / AP inventory (L0)',
  {},
  async () => jsonResult(await platformGet('/api/v1/network/devices')),
)

server.tool(
  'get_network_clients',
  'Client count per VLAN / SSID summary (L0)',
  {},
  async () => jsonResult(await platformGet('/api/v1/network/clients')),
)

server.tool(
  'apply_network_firewall',
  'L1 idempotent re-sync missing Bifrost ZBF policies — operator token required',
  { include_default_deny: z.boolean().optional() },
  async ({ include_default_deny }) =>
    jsonResult(
      await platformPost('/api/v1/network/firewall/apply', {
        include_default_deny: include_default_deny ?? false,
      }),
    ),
)

async function main() {
  const transport = new StdioServerTransport()
  await server.connect(transport)
}

main().catch(err => {
  console.error(err)
  process.exit(1)
})
