/**
 * Trade environment gateway entrypoints (Dev / STG / Prod) — single source of truth.
 *
 * Mirrors the Platform Release `PLATFORM_*_URLS` pattern so the Trade Release →
 * Delivery view can show always-visible external entrypoints.
 *
 * Today each gateway resolves to a K3s node IP + ingress:
 *   - DEV  → node .73 : 30882 (nginx NodePort)
 *   - STG  → Traefik @ .73 : 80, Host trade-stg.bifrost.lan
 *   - PROD → Traefik @ .70 : 80, Host trade.bifrost.lan
 *
 * VLAN / kube-vip migration (see architecture → networkUpgradeCatalog):
 *   When the LAN gets a single virtual IP (kube-vip), set `TRADE_INGRESS_VIP`
 *   to that address. All three environments then resolve through the one VIP
 *   (NodePort preserved, e.g. `VIP:30880`) without touching call sites. Keep
 *   these values in sync with `config/clusters.yaml` (`*_smoke.gateway_url`).
 */

/** Set to the kube-vip VIP (e.g. '192.168.10.100') once the VLAN upgrade lands; null = use per-node IPs. */
export const TRADE_INGRESS_VIP: string | null = null

export type TradeEnvTier = 'DEV' | 'STG' | 'PROD'

interface TradeEnvDef {
  env: TradeEnvTier
  label: string
  /** Current K3s node IP that serves this gateway (ignored when a VIP is set). */
  nodeHost: string
  /** Gateway port (Traefik :80 for STG; nginx NodePort for dev/prod). */
  port: number
  /** Traefik Host-based ingress hostname (STG). Browser URL uses this when set. */
  ingressHost?: string
}

const TRADE_ENV_DEFS: readonly TradeEnvDef[] = [
  { env: 'DEV', label: 'Trade DEV', nodeHost: '192.168.10.73', port: 30882 },
  { env: 'STG', label: 'Trade STG', nodeHost: '192.168.10.73', port: 80, ingressHost: 'trade-stg.bifrost.lan' },
  { env: 'PROD', label: 'Trade PROD', nodeHost: '192.168.10.70', port: 80, ingressHost: 'trade.bifrost.lan' },
] as const

export interface TradeEnvAccess {
  env: TradeEnvTier
  label: string
  /** Full gateway base URL (trailing slash). */
  gateway: string
  /** Host actually used (VIP when set, otherwise the per-node IP). */
  host: string
}

function resolveHost(def: TradeEnvDef): string {
  return TRADE_INGRESS_VIP ?? def.nodeHost
}

export const TRADE_ENV_ACCESS: readonly TradeEnvAccess[] = TRADE_ENV_DEFS.map(def => {
  const host = resolveHost(def)
  const gateway = def.ingressHost
    ? `http://${def.ingressHost}/`
    : `http://${host}:${def.port}/`
  return {
    env: def.env,
    label: def.label,
    gateway,
    host: def.ingressHost ?? host,
  }
})

/** True once a single VIP fronts all Trade gateways (drives a "VIP" hint in the UI). */
export const TRADE_INGRESS_USES_VIP = TRADE_INGRESS_VIP !== null
