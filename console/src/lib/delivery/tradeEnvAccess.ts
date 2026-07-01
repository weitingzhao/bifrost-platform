/**
 * Trade environment gateway entrypoints (Dev / STG / Prod) — single source of truth.
 *
 * IP-only LAN access (no /etc/hosts required):
 *   - DEV  → http://192.168.10.73:30882/
 *   - STG  → http://192.168.10.73:30880/
 *   - PROD → http://192.168.10.70/
 *
 * Traefik Host hostnames (trade-*.bifrost.lan) remain on :80 for future UniFi DNS;
 * see TRADE_HOSTNAME_GATEWAYS.
 */

export const TRADE_INGRESS_VIP: string | null = null

export type TradeEnvTier = 'DEV' | 'STG' | 'PROD'

interface TradeEnvDef {
  env: TradeEnvTier
  label: string
  nodeHost: string
  port: number
  /** Optional Traefik Host ingress (requires LAN DNS or /etc/hosts). */
  ingressHost?: string
}

const TRADE_ENV_DEFS: readonly TradeEnvDef[] = [
  { env: 'DEV', label: 'Trade DEV', nodeHost: '192.168.10.73', port: 30882, ingressHost: 'trade-dev.bifrost.lan' },
  { env: 'STG', label: 'Trade STG', nodeHost: '192.168.10.73', port: 30880, ingressHost: 'trade-stg.bifrost.lan' },
  { env: 'PROD', label: 'Trade PROD', nodeHost: '192.168.10.70', port: 80, ingressHost: 'trade.bifrost.lan' },
] as const

export interface TradeEnvAccess {
  env: TradeEnvTier
  label: string
  /** Browser URL (IP:port — works without /etc/hosts). */
  gateway: string
  nodeHost: string
  port: number
  ingressHost?: string
}

function resolveNodeHost(def: TradeEnvDef): string {
  return TRADE_INGRESS_VIP ?? def.nodeHost
}

export const TRADE_ENV_ACCESS: readonly TradeEnvAccess[] = TRADE_ENV_DEFS.map(def => {
  const nodeHost = resolveNodeHost(def)
  const gateway =
    def.port === 80 ? `http://${nodeHost}/` : `http://${nodeHost}:${def.port}/`
  return {
    env: def.env,
    label: def.label,
    gateway,
    nodeHost,
    port: def.port,
    ingressHost: def.ingressHost,
  }
})

/** Hostname URLs on Traefik :80 (after UniFi DNS or /etc/hosts). */
export const TRADE_HOSTNAME_GATEWAYS = TRADE_ENV_DEFS.filter(d => d.ingressHost).map(
  d => `http://${d.ingressHost}/`,
)

export const TRADE_INGRESS_USES_VIP = TRADE_INGRESS_VIP !== null
