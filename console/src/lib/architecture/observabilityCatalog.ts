/**
 * Observability hub — Governance catalog (authoritative IA + ownership).
 * Ops Console → Mission Control → Observability
 *
 * Code SSOT: console/src/lib/observability/*
 */

import {
  OBSERVABILITY_REGISTRY_SOURCE,
  OBSERVABILITY_REGISTRY_VERSION,
  SIGNAL_REGISTRY,
} from '@/lib/observability/signalRegistry'
import { GRAFANA_DASHBOARD_CATALOG } from '@/lib/observability/dashboardCatalog'

export const OBSERVABILITY_CATALOG_VERSION = '2026-07-21'
export const OBSERVABILITY_CATALOG_SOURCE =
  'console/src/lib/architecture/observabilityCatalog.ts'

export const OBSERVABILITY_OWNERSHIP = {
  hubRoute: 'observability',
  hubPlane: 'Mission Control',
  satelliteRuntimeRoute: 'satellite-telemetry',
  clusterLayerABRoute: 'cluster',
  grafanaRole: 'Deep evidence / diagnostics — Console does not duplicate full dashboards',
  verdictSource: 'console/src/lib/observability/observabilityViewModel.ts',
  d10Note: 'Live trading remains BLOCKED (D10). Observability is read-only monitoring.',
  iaBoundary:
    'Health hub only — not Mission launch (TCC) and not bay posture cockpit (Control Room).',
} as const

export const OBSERVABILITY_VERDICT_RULES = [
  'Verdicts: HEALTHY / DEGRADED / CRITICAL / UNKNOWN / NOT OBSERVED',
  'Signal may be EXPECTED OFF (neutral — never fails required rollup)',
  'Only required signals affect overall / domain verdicts',
  'K8s workload / CPU / memory default to evidence',
  'Alerts affect verdict only after domain + severity mapping',
  'Historical Defects do not participate',
  'Shared dependencies counted once; mark affected domains',
  'Mission Control / Governance without runtime contract → NOT OBSERVED (never invent metrics)',
] as const

export function buildObservabilityLlmPack(): string {
  const lines: string[] = [
    '## Observability Hub (Mission Control)',
    `# Source: ${OBSERVABILITY_CATALOG_SOURCE} v${OBSERVABILITY_CATALOG_VERSION}`,
    `# Signal registry: ${OBSERVABILITY_REGISTRY_SOURCE} v${OBSERVABILITY_REGISTRY_VERSION}`,
    '',
    `Hub: Mission Control → Observability (\`${OBSERVABILITY_OWNERSHIP.hubRoute}\`)`,
    `Satellite scoped detail: Satellite → Satellite Runtime (\`${OBSERVABILITY_OWNERSHIP.satelliteRuntimeRoute}\`)`,
    `Layer A/B install: Rocket → Cluster (\`${OBSERVABILITY_OWNERSHIP.clusterLayerABRoute}\`)`,
    `Grafana: ${OBSERVABILITY_OWNERSHIP.grafanaRole}`,
    `Verdict SSOT: \`${OBSERVABILITY_OWNERSHIP.verdictSource}\``,
    OBSERVABILITY_OWNERSHIP.d10Note,
    `IA: ${OBSERVABILITY_OWNERSHIP.iaBoundary}`,
    '',
    '### Verdict rules',
    ...OBSERVABILITY_VERDICT_RULES.map(r => `- ${r}`),
    '',
    '### Signal registry (required / evidence)',
    ...SIGNAL_REGISTRY.map(
      s =>
        `- \`${s.id}\` · ${s.domain} · ${s.scope} · ${s.role} · source=${s.source}` +
        (s.optionalContract ? ' · optionalContract' : ''),
    ),
    '',
    '### Grafana dashboard catalog',
    ...GRAFANA_DASHBOARD_CATALOG.map(
      d =>
        `- **${d.title}** (\`${d.id}\`) · ${d.domain} · ${d.env} · uid=${d.uid ?? 'unavailable'} — ${d.purpose}`,
    ),
  ]
  return lines.join('\n')
}
