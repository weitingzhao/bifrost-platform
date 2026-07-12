/**
 * Trade environment gateway entrypoints (Dev / STG / Prod) — single source of truth.
 *
 * Preferred path (UniFi DNS → VIP → Traefik Host):
 *   - DEV  → http://dev.trader.bifrost.lan/
 *   - STG  → http://stg.trader.bifrost.lan/
 *   - PROD → http://trader.bifrost.lan/
 *
 * IP escape (NodePort / node :80) still listed when VIP is unset; with VIP set,
 * IP gateways resolve through TRADE_INGRESS_VIP.
 */

export const TRADE_INGRESS_VIP: string | null = '192.168.10.100'

export type TradeEnvTier = 'DEV' | 'STG' | 'PROD'

interface TradeEnvDef {
  env: TradeEnvTier
  label: string
  nodeHost: string
  port: number
  /** Traefik Host ingress (requires LAN DNS or /etc/hosts). */
  ingressHost?: string
}

const TRADE_ENV_DEFS: readonly TradeEnvDef[] = [
  { env: 'DEV', label: 'Trade DEV', nodeHost: '192.168.10.73', port: 30882, ingressHost: 'dev.trader.bifrost.lan' },
  { env: 'STG', label: 'Trade STG', nodeHost: '192.168.10.73', port: 30880, ingressHost: 'stg.trader.bifrost.lan' },
  { env: 'PROD', label: 'Trade PROD', nodeHost: '192.168.10.70', port: 80, ingressHost: 'trader.bifrost.lan' },
] as const

export interface TradeEnvAccess {
  env: TradeEnvTier
  label: string
  /** Browser URL (VIP or node IP — escape without DNS). */
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
