/**
 * Map Prometheus alerts → Apollo domain + severity.
 *
 * Unmapped alerts never affect system/domain verdicts (Owner rule).
 * Only alerts with both domain and severity mapped can degrade/critical.
 */

import type { SystemDomainId } from '@/lib/architecture/systemDomainCatalog'
import type { AlertSeverity, MappedAlert, ObservabilityEnvId } from './types'

export type RawAlertInput = {
  labels?: Record<string, string>
  annotations?: Record<string, string>
  state?: string
  active_at?: string
  value?: string
}

type AlertRule = {
  /** Match alertname (case-insensitive substring or exact). */
  nameMatch: RegExp
  domain: SystemDomainId
  severity: AlertSeverity
}

/**
 * Whitelist mapping — extend carefully; do not invent domains from free-form labels alone
 * unless severity+domain can both be resolved.
 */
const ALERT_RULES: AlertRule[] = [
  { nameMatch: /^Watchdog$/i, domain: 'rocket', severity: 'info' },
  { nameMatch: /TargetDown|ScrapeFailed|Prometheus/i, domain: 'rocket', severity: 'critical' },
  { nameMatch: /KubeNode|KubePodCrash|KubeDeployment|NodeNotReady|NodeFilesystem/i, domain: 'rocket', severity: 'warning' },
  { nameMatch: /CPUThrottling|HighCPU|HighMemory|MemoryPressure/i, domain: 'rocket', severity: 'warning' },
  { nameMatch: /Redis|CNPG|Postgres|PostgreSQL|DiskSpace/i, domain: 'ground-systems', severity: 'critical' },
  { nameMatch: /Http5xx|ApiError|ApiLatency|TradeApi/i, domain: 'satellite', severity: 'critical' },
  { nameMatch: /IbGateway|IBGateway|TwsDisconnect/i, domain: 'subcontractors', severity: 'critical' },
  { nameMatch: /AgentBridge|RemediationRunner|Hermes/i, domain: 'engineer', severity: 'warning' },
]

function severityFromLabels(labels: Record<string, string>): AlertSeverity | null {
  const raw = (labels.severity ?? labels.Severity ?? '').toLowerCase()
  if (raw === 'critical' || raw === 'error' || raw === 'page') return 'critical'
  if (raw === 'warning' || raw === 'warn') return 'warning'
  if (raw === 'info' || raw === 'none') return 'info'
  return null
}

function domainFromLabels(labels: Record<string, string>): SystemDomainId | null {
  const domain = (labels.domain ?? labels.apollo_domain ?? '').toLowerCase()
  const allowed: SystemDomainId[] = [
    'mission-control',
    'rocket',
    'ground-systems',
    'satellite',
    'subcontractors',
    'engineer',
    'governance',
  ]
  if ((allowed as string[]).includes(domain)) return domain as SystemDomainId

  const ns = labels.namespace ?? labels.exported_namespace ?? ''
  if (/^bifrost-(dev|stg|prod)$/.test(ns)) return 'satellite'
  if (ns === 'data' || ns === 'monitoring') {
    return ns === 'data' ? 'ground-systems' : 'rocket'
  }
  return null
}

function envFromLabels(labels: Record<string, string>): ObservabilityEnvId {
  const ns = labels.namespace ?? labels.exported_namespace ?? ''
  if (ns === 'bifrost-dev') return 'dev'
  if (ns === 'bifrost-stg') return 'stg'
  if (ns === 'bifrost-prod') return 'prod'
  const env = (labels.env ?? labels.environment ?? '').toLowerCase()
  if (env === 'dev' || env === 'stg' || env === 'prod') return env
  return 'shared'
}

function matchRule(alertname: string): AlertRule | undefined {
  return ALERT_RULES.find(r => r.nameMatch.test(alertname))
}

/**
 * Map a single Prometheus alert. Returns `mapped: false` when domain or severity
 * cannot be resolved — those alerts are listed but do not affect verdicts.
 */
export function mapAlert(raw: RawAlertInput, index: number): MappedAlert {
  const labels = raw.labels ?? {}
  const annotations = raw.annotations ?? {}
  const name = labels.alertname ?? labels.alert ?? `alert-${index}`
  const rule = matchRule(name)
  const labelSeverity = severityFromLabels(labels)
  const labelDomain = domainFromLabels(labels)

  // Prometheus alert labels are ground truth for severity; rule severity is
  // only a fallback when the label carries none (e.g. TargetDown without a
  // severity label defaults to the rule's critical).
  const severity = labelSeverity ?? rule?.severity
  const domain = rule?.domain ?? labelDomain
  const mapped = severity != null && domain != null && severity !== 'info'

  const summary =
    annotations.summary ??
    annotations.description ??
    annotations.message ??
    `${name} (${raw.state ?? 'unknown'})`

  return {
    id: `${name}:${labels.instance ?? labels.pod ?? labels.namespace ?? index}`,
    name,
    state: (raw.state ?? 'unknown').toLowerCase(),
    severity: severity ?? 'unknown',
    domain,
    mapped,
    env: envFromLabels(labels),
    summary,
    activeAt: raw.active_at,
    labels,
    annotations,
  }
}

export function mapAlerts(raws: RawAlertInput[]): MappedAlert[] {
  return raws.map((r, i) => mapAlert(r, i))
}

/** Firing alerts that are allowed to influence verdict. */
export function verdictAffectingAlerts(alerts: MappedAlert[]): MappedAlert[] {
  return alerts.filter(
    a =>
      a.mapped &&
      a.domain != null &&
      (a.state === 'firing' || a.state === 'pending') &&
      (a.severity === 'critical' || a.severity === 'warning'),
  )
}

export function firingDurationMs(activeAt: string | undefined, nowMs: number): number | null {
  if (activeAt == null || activeAt === '') return null
  const t = Date.parse(activeAt)
  if (Number.isNaN(t)) return null
  return Math.max(0, nowMs - t)
}
