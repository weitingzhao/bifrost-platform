import type { HermesGatewayHealth } from './agentTypes'

/** Nous Hermes Chat UI — Analysis Desk deep link (LAN). */
export const HERMES_CHAT_UI_URL = 'http://192.168.10.50:9119/chat'

export async function fetchHermesGatewayHealth(): Promise<HermesGatewayHealth> {
  const r = await fetch('/api/v1/agent/hermes/health')
  if (!r.ok) throw new Error(`hermes health: HTTP ${r.status}`)
  return r.json() as Promise<HermesGatewayHealth>
}
