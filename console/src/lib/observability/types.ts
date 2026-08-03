/**
 * Observability hub — shared verdict / signal types.
 *
 * Single source of truth for Mission Control → Observability.
 * Do not re-derive overall/domain verdicts on Satellite / Cluster pages.
 */

import type { SystemDomainId } from '@/lib/architecture/systemDomainCatalog'
import type { RemediationTrack } from '@/lib/cluster/clusterFailureTriage'

/** System-level health verdict (display labels are uppercase). */
export type ObservabilityVerdict =
  | 'healthy'
  | 'degraded'
  | 'critical'
  | 'unknown'
  | 'not_observed'

export const VERDICT_LABELS: Record<ObservabilityVerdict, string> = {
  healthy: 'HEALTHY',
  degraded: 'DEGRADED',
  critical: 'CRITICAL',
  unknown: 'UNKNOWN',
  not_observed: 'NOT OBSERVED',
}

/** Per-signal state. EXPECTED OFF is neutral (never fails a required rollup). */
export type SignalState =
  | 'healthy'
  | 'degraded'
  | 'critical'
  | 'unknown'
  | 'not_observed'
  | 'expected_off'

export type SignalRole = 'required' | 'evidence'

export type SignalScope = 'env' | 'shared'

export type SignalSourceKind =
  | 'telemetry_metric'
  | 'telemetry_target'
  | 'telemetry_alert'
  | 'cluster_observability'
  | 'cluster_metrics'
  | 'matrix'
  | 'bus_deep'
  | 'ib_gateway'
  | 'agent_bridge'
  | 'self_health'
  | 'remediation'
  | 'none'

export type ObservabilityEnvId = 'dev' | 'stg' | 'prod' | 'shared'

export type AlertSeverity = 'critical' | 'warning' | 'info' | 'unknown'

export type SignalDef = {
  id: string
  label: string
  domain: SystemDomainId
  scope: SignalScope
  role: SignalRole
  source: SignalSourceKind
  /** When true, absence of data → NOT OBSERVED (never fake HEALTHY). */
  optionalContract?: boolean
  detailRoute?: string
  grafanaDashboardId?: string
  /** Affected domains when this shared signal fails (shared deps counted once). */
  affectsDomains?: SystemDomainId[]
  description?: string
}

export type EvaluatedSignal = {
  def: SignalDef
  state: SignalState
  summary: string
  freshnessMs?: number
  stale?: boolean
  env: ObservabilityEnvId
  evidence?: string
  /** Linked alert ids / target scrape pools contributing to this signal. */
  linkedIds?: string[]
}

/** Expected vs Actual gap for a required signal. */
export type SignalGap = 'ok' | 'fail' | 'blind' | 'by_design'

/**
 * Domain probeability for Observability hub layout / gap rollups.
 * - runtime: participates in the health grid + system gap meta
 * - reference: Apollo taxonomy plane with no runtime contract (by design)
 */
export type DomainProbeability = 'runtime' | 'reference'

export type GapSummary = {
  ok: number
  fail: number
  blind: number
  byDesign: number
  total: number
}

export type DomainHealth = {
  domain: SystemDomainId
  label: string
  verdict: ObservabilityVerdict
  reason: string
  coverage: { observed: number; required: number; evidence: number }
  /** Required-signal Expected vs Actual rollup (ok / fail / blind / by_design). */
  gapSummary: GapSummary
  /**
   * Whether this domain has a reliable runtime probe contract.
   * Reference domains stay in taxonomy but are demoted from the main health grid.
   */
  probeability: DomainProbeability
  alertCount: number
  envScope: 'env' | 'shared' | 'mixed' | 'none'
  signals: EvaluatedSignal[]
  sharedDependencyIds: string[]
}

/** Attention Inspect CTA — assisted remediation entry (not a second execution engine). */
export type AttentionRemediationCta = 'agent_fix' | 'diagnose' | 'manual'

export type AttentionItem = {
  id: string
  severity: AlertSeverity
  domain: SystemDomainId
  env: ObservabilityEnvId
  signalId: string
  signalLabel: string
  since?: string
  owner: string
  action: string
  summary: string
  /** Structured triage + assisted remediation classification. */
  triage: {
    whatHappened: string
    whyVerdictChanged: string
    affectedDomains: SystemDomainId[]
    evidence: string
    recommendedDestination: string
    detailRoute?: string
    grafanaUrl?: string | null
    /** Remediation track — aligned with Cluster Failure Triage. */
    track: RemediationTrack
    playbookId?: string
    cta: AttentionRemediationCta
    trackReason: string
    suggestedAction: string
  }
}

export type ScrapeTargetView = {
  id: string
  job: string
  instance: string
  /** Kubernetes node name when present (kubelet / node-exporter). */
  node?: string
  /** Pod name when present. */
  pod?: string
  /** Scrape metrics path (e.g. /metrics, /metrics/cadvisor). */
  metricsPath?: string
  namespace?: string
  health: 'up' | 'down' | 'unknown'
  lastScrape?: string
  lastError?: string
  role: SignalRole
  domain: SystemDomainId
  env: ObservabilityEnvId
  /**
   * DOWN on an elastic standby host — expected off, not a fail/red signal.
   * UI must not use error/danger styling for these rows.
   */
  expectedOff?: boolean
}

export type MappedAlert = {
  id: string
  name: string
  state: string
  severity: AlertSeverity
  domain: SystemDomainId | null
  /** null = unmapped — does not affect verdict until domain+severity mapped. */
  mapped: boolean
  env: ObservabilityEnvId
  summary: string
  activeAt?: string
  labels: Record<string, string>
  annotations: Record<string, string>
  /**
   * Elastic / WOL standby node alert — expected when no GPU/warehouse demand.
   * Neutral: never Attention WARNING and never degrades Rocket verdict.
   */
  standbyNeutral?: boolean
}

export type SystemVerdict = {
  overall: ObservabilityVerdict
  label: string
  /** Runtime-domain verdict counts only (reference planes excluded). */
  domainCounts: Record<ObservabilityVerdict, number>
  /** Apollo reference domains demoted from the health grid (MC / Governance). */
  referenceDomainCount: number
  firingAlerts: number
  mappedFiringAlerts: number
  primaryCause: string
  env: ObservabilityEnvId
  freshnessMs: number | null
  stale: boolean
  generatedAt: string
}

export type DependencyPathHop = {
  id: string
  label: string
  scope: SignalScope
  state: SignalState
  summary: string
}

export type GoldenSignalRow = {
  id: string
  label: string
  unit?: string
  status: 'ok' | 'empty' | 'error' | 'unknown'
  valueLabel: string
  detail?: string
}

export type GrafanaSoloEmbed = {
  url: string
  title: string
  height: number
}

export type ScrapeTargetsRollup = {
  /** True when no unexpected DOWN/unknown — standby expected-off does not fail this. */
  quiet: boolean
  /** Compact section description (English). */
  label: string
}

export type SelectedDomainDetail = {
  domain: SystemDomainId
  dependencyPath: DependencyPathHop[]
  goldenSignals: GoldenSignalRow[]
  alerts: MappedAlert[]
  scrapeTargets: ScrapeTargetView[]
  scrapeRollup: ScrapeTargetsRollup
  detailLinks: { label: string; route: string }[]
  grafanaLinks: { label: string; url: string | null; available: boolean }[]
  /** Primary dashboard solo panel embed; null when catalog has no soloPanel or URL unavailable. */
  soloEmbed: GrafanaSoloEmbed | null
}

export type GrafanaDashboardEntry = {
  id: string
  title: string
  domain: SystemDomainId
  env: ObservabilityEnvId | 'all'
  purpose: string
  /** Grafana uid path segment — null means unavailable (no bad link). */
  uid: string | null
  slug: string
  /**
   * Preferred `var-namespace` for deep links. When set, URL builder uses this
   * instead of Trade NS (e.g. Ground/IB → `data`, Agent → platform NS).
   * Omit to fall back to TRADE_NS[env] (Satellite) or no namespace.
   */
  defaultNamespace?: string
  /**
   * When true, deep links omit `var-namespace` entirely (Rocket stock boards
   * that are cluster-scoped and do not accept a namespace template var).
   */
  suppressNamespace?: boolean
  /**
   * When true, deep links omit `var-env` (cluster-scoped stock boards that
   * have no Trade env template variable).
   */
  suppressEnv?: boolean
  /**
   * Optional single-panel embed for Selected Domain (Grafana /d-solo).
   * Omit or leave unset → no iframe.
   */
  soloPanel?: { panelId: number; title: string; height?: number }
}

export type ObservabilityViewModel = {
  system: SystemVerdict
  domains: DomainHealth[]
  attention: AttentionItem[]
  selected: SelectedDomainDetail
  dashboards: Array<GrafanaDashboardEntry & { available: boolean; url: string | null }>
  layerBStatus: string
  prometheusConfigured: boolean
  grafanaBaseUrl: string | null
}
