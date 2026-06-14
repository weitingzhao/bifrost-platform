/** MkDocs handbooks — separate dev servers; see bifrost-platform/scripts/start_docs.sh */

export const PLATFORM_DOCS_BASE =
  import.meta.env.VITE_PLATFORM_DOCS_URL ?? 'http://127.0.0.1:8060'

export const INFRA_DOCS_BASE = import.meta.env.VITE_INFRA_DOCS_URL ?? 'http://127.0.0.1:8050'

export const DOC_LINKS = {
  platformHome: PLATFORM_DOCS_BASE,
  northStar: `${PLATFORM_DOCS_BASE}/NORTH_STAR/`,
  architecture: `${PLATFORM_DOCS_BASE}/ARCHITECTURE/`,
  agentModes: `${PLATFORM_DOCS_BASE}/AGENT_MODES/`,
  tradeContract: `${PLATFORM_DOCS_BASE}/TRADE_CONTRACT/`,
  infraHome: INFRA_DOCS_BASE,
} as const
