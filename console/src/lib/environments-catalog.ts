/**
 * Bifrost Ops — environments & architecture catalog.
 *
 * Static catalog for LLM packs and Runtime Map registry (probe targets, scope, hardware).
 * Milestones and decisions: authoritative via config/ops-context.yaml + GET /api/v1/context.
 *
 * Live environment status: Control Room + Runtime Map. Governance UI: Blueprint, Standards, Agent Protocol.
 */

export const CATALOG_VERSION = '2026-06-29-ai-compute-strategy'

import type { ScopeRow, HardwareRow } from '@/domains/foundation/environment-catalog/contracts'
export type { ScopeRow, FlowRowStatus, FlowRow, HardwareRow, PlatformPhase } from '@/domains/foundation/environment-catalog/contracts'
import { foundationScopeRows, foundationHardware } from '@/domains/foundation/environment-catalog/records'
import { tenantScopeRows, tenantHardware } from '@/domains/tenant/environment/catalog'

export const SCOPE_ROWS: ScopeRow[] = [
  {
    tag: 'PLATFORM',
    component: 'Bifrost Ops Platform (control plane)',
    technology: 'Go API :8780 · React Ops Console :5180 · Dense UI tokens',
    notes:
      'Control plane — Go API :8780, React Ops Console :5180, MCP tools. Probes matrix/topology/spine; no Monitor POST /control/* or ib:operator:cmd.',
  },
  tenantScopeRows['TRADE-FE'],
  tenantScopeRows['TRADE-API'],
  tenantScopeRows['WORKER'],
  {
    tag: 'SOCKET',
    component: 'bifrost-trade-socket (RETIRED / reference-only)',
    technology: 'Python · legacy IB ingestor / account / operator (reference)',
    notes:
      'Wave 14G-F: production IB edge is Platform IB Gateway Plugin → redis-ib (IBGP). Do not start trade-socket for Inner Loop or K3s. Repo kept as reference until Phase 3 archive. Compose default path still pending Phase 1 profile cutover.',
  },
  tenantScopeRows['CORE'],
  tenantScopeRows['INFRA'],
  foundationScopeRows['PG'],
  tenantScopeRows['REDIS'],
  {
    tag: 'TWS',
    component: 'Interactive Brokers TWS / Gateway',
    technology: 'Win11 ×2 (Host + Secondary) — cluster-external forever',
    notes: 'R-DV3: Dev/Prod distinct client_id; one auto-trading Engine per IB account. D4: auto-order switch deferred.',
  },
  foundationScopeRows['GITHUB'],
  foundationScopeRows['K3S'],
  foundationScopeRows['OBSERVE'],
  {
    tag: 'AI',
    component: 'Inference & MCP (target)',
    technology: 'Ollama on gpu-server (4090) · mcp-server-kubernetes · bifrost-ops-mcp',
    notes: 'L0/L1 ops only; research RAG CronJob read-only. Forbidden: trade write paths for platform AI.',
  },
  foundationScopeRows['AGENT'],
]

export const HARDWARE_ROWS: HardwareRow[] = [
  foundationHardware['mini-pc-a'],
  foundationHardware['ubt-k3s-06'],
  foundationHardware['mini-pc-c'],
  foundationHardware['mac-mini-1'],
  foundationHardware['mac-mini-2'],
  foundationHardware['gpu-server'],
  foundationHardware['ubt-k3s-04'],
  foundationHardware['ubt-k3s-05'],
  tenantHardware['win11-host'],
  tenantHardware['win11-secondary'],
  foundationHardware['macbook'],
]

/** Spine section from GET /api/v1/context (authoritative milestones). */
