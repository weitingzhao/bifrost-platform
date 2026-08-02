/**
 * Map Prometheus alerts → Apollo domain + severity.
 *
 * Unmapped alerts never affect system/domain verdicts (Owner rule).
 * Only alerts with both domain and severity mapped can degrade/critical.
 *
 * Elastic standby formula (Owner):
 *   WARNING = demand(replicas > 0) AND node offline
 *   Standby + no demand → NEUTRAL (standbyNeutral); never Attention WARNING.
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

/** Host identity for elastic standby matching (node name and/or InternalIP). */
export type StandbyNodeRef = {
  name: string
  internalIp?: string
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

/**
 * Alertnames that are expected noise when the target node is elastic standby
 * (powered off / NotReady with no compute demand).
 */
const ELASTIC_STANDBY_ALERT_NAMES =
  /^(KubeNodeNotReady|KubeNodeUnreachable|KubeletInstanceUnreachable|KubeDaemonSetRolloutStuck|KubeDaemonSetMisScheduled|KubePodNotReady)$/i

/**
 * All-node DaemonSets that schedule on every worker — when one elastic standby
 * is off, kube-state-metrics emits Pod/DS alerts without a `node` label.
 */
const STANDBY_NOISE_DAEMONSETS =
  /^(kube-prometheus-stack-prometheus-node-exporter|promtail|loki-canary|svclb-traefik(?:-.+)?)$/i

const STANDBY_NOISE_POD_PREFIX =
  /^(kube-prometheus-stack-prometheus-node-exporter-|promtail-|loki-canary-|svclb-traefik-)/i

export type StandbyDownTarget = {
  job?: string
  instance?: string
  health?: string
}

function alertNodeHints(alert: MappedAlert): string[] {
  const labels = alert.labels
  const hints: string[] = []
  for (const key of ['node', 'nodename', 'kubernetes_node', 'instance', 'exported_instance'] as const) {
    const v = labels[key]
    if (v != null && v !== '') hints.push(v)
  }
  return hints
}

/** True when a scrape/alert host string refers to a standby node name or IP. */
export function hostMatchesStandbyNode(host: string, standbyNodes: StandbyNodeRef[]): boolean {
  const raw = host.trim().toLowerCase()
  if (raw === '') return false
  // Strip scrape port (node-exporter :9100, kubelet :10250, …).
  const hostOnly = raw.replace(/:\d+$/, '')
  for (const n of standbyNodes) {
    const name = n.name.trim().toLowerCase()
    const ip = (n.internalIp ?? '').trim().toLowerCase()
    if (name !== '' && (raw === name || hostOnly === name || raw.startsWith(`${name}.`))) {
      return true
    }
    if (ip !== '' && (raw === ip || hostOnly === ip || raw.startsWith(`${ip}:`))) {
      return true
    }
  }
  return false
}

function isNodeScrapeJob(job: string): boolean {
  return /node-exporter|node_exporter|kubelet/i.test(job)
}

/**
 * Aggregate TargetDown (no per-target instance) — neutralize when every DOWN
 * node-exporter/kubelet scrape target maps to a standby host. Without targets,
 * fall back to job match + presence of standby (1 elastic node ≈ 1/N down).
 */
function isAggregateStandbyTargetDown(
  alert: MappedAlert,
  standbyNodes: StandbyNodeRef[],
  downTargets: StandbyDownTarget[] | undefined,
): boolean {
  const job = (alert.labels.job ?? alert.labels.scrape_job ?? '').toLowerCase()
  const blob = `${job} ${alert.labels.instance ?? ''} ${alert.summary}`
  if (!isNodeScrapeJob(blob)) return false

  const hints = alertNodeHints(alert)
  // Per-instance TargetDown still uses host match in the caller.
  if (hints.length > 0) return false

  if (downTargets != null && downTargets.length > 0) {
    const downNodeScrapes = downTargets.filter(t => {
      const health = (t.health ?? '').toLowerCase()
      if (health !== '' && health !== 'down') return false
      return isNodeScrapeJob(t.job ?? '')
    })
    if (downNodeScrapes.length === 0) return false
    return downNodeScrapes.every(
      t => t.instance != null && t.instance !== '' && hostMatchesStandbyNode(t.instance, standbyNodes),
    )
  }

  // No target inventory — neutralize aggregate node scrape TargetDown while
  // at least one elastic standby is registered (typical 1/N scrape loss).
  return isNodeScrapeJob(job)
}

function isStandbyDaemonSetNoise(alert: MappedAlert): boolean {
  const name = alert.name
  if (/^KubeDaemonSet(RolloutStuck|MisScheduled)$/i.test(name)) {
    const ds = alert.labels.daemonset ?? alert.labels.daemon_set ?? ''
    return STANDBY_NOISE_DAEMONSETS.test(ds)
  }
  if (/^KubePodNotReady$/i.test(name)) {
    const pod = alert.labels.pod ?? ''
    return STANDBY_NOISE_POD_PREFIX.test(pod)
  }
  return false
}

/**
 * Whether this alert is expected elastic-standby noise for the given standby node set.
 * Callers should pass only `elastic_mode === 'standby'` nodes (not degraded/demand).
 */
export function isElasticStandbyAlert(
  alert: MappedAlert,
  standbyNodes: StandbyNodeRef[],
  downTargets?: StandbyDownTarget[],
): boolean {
  if (standbyNodes.length === 0) return false
  const name = alert.name
  const isTargetDown = /^TargetDown$/i.test(name)
  if (!ELASTIC_STANDBY_ALERT_NAMES.test(name) && !isTargetDown) return false

  // Direct host match (node= / instance=IP:port).
  if (alertNodeHints(alert).some(h => hostMatchesStandbyNode(h, standbyNodes))) {
    if (isTargetDown) {
      const job = (alert.labels.job ?? alert.labels.scrape_job ?? '').toLowerCase()
      const blob = `${job} ${alert.labels.instance ?? ''} ${alert.summary}`
      if (
        !isNodeScrapeJob(blob) &&
        !/:\d{2,5}$/.test(alert.labels.instance ?? '')
      ) {
        return false
      }
    }
    return true
  }

  if (isTargetDown) {
    return isAggregateStandbyTargetDown(alert, standbyNodes, downTargets)
  }

  // Pod / DaemonSet alerts often omit `node` — match known all-node DS names.
  return isStandbyDaemonSetNoise(alert)
}

/** Annotate mapped alerts with standbyNeutral when they hit elastic standby hosts. */
export function annotateStandbyAlerts(
  alerts: MappedAlert[],
  standbyNodes: StandbyNodeRef[],
  downTargets?: StandbyDownTarget[],
): MappedAlert[] {
  if (standbyNodes.length === 0) return alerts
  return alerts.map(a => {
    if (!isElasticStandbyAlert(a, standbyNodes, downTargets)) return a
    return { ...a, standbyNeutral: true }
  })
}

/** Firing alerts that are allowed to influence verdict. */
export function verdictAffectingAlerts(alerts: MappedAlert[]): MappedAlert[] {
  return alerts.filter(
    a =>
      a.mapped &&
      !a.standbyNeutral &&
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
