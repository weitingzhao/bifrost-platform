/** MkDocs staging site + infra handbook links. Governance: Ops Console → Architecture catalogs. */

export const PLATFORM_DOCS_BASE =
  import.meta.env.VITE_PLATFORM_DOCS_URL ?? 'http://127.0.0.1:8060'

export const INFRA_DOCS_BASE = import.meta.env.VITE_INFRA_DOCS_URL ?? 'http://127.0.0.1:8050'

export const DOC_LINKS = {
  /** MkDocs staging home (draft notes — not authoritative for governance) */
  platformHome: PLATFORM_DOCS_BASE,
  stagingPolicy: `${PLATFORM_DOCS_BASE}/STAGING/`,
  infraHome: INFRA_DOCS_BASE,
  /** Governance: Ops Console → Architecture → K3s Architecture §10 checkpoints */
  k3sArchitecture: 'architecture/k3s-architecture',
  platformRoadmap: 'architecture/roadmap',
} as const
