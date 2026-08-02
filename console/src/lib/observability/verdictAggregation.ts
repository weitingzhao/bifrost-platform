/**
 * Deterministic verdict aggregation for Observability hub.
 *
 * Rules:
 * - Only required signals affect overall / domain verdicts
 * - EXPECTED OFF is neutral
 * - Optional-contract NOT OBSERVED is neutral (Owner decision 2026-07-21):
 *   signals without a reliable runtime contract yet must not pin a domain to UNKNOWN
 * - UNKNOWN / NOT OBSERVED never masquerade as HEALTHY
 * - CRITICAL > DEGRADED > UNKNOWN > NOT OBSERVED > HEALTHY
 * - Shared dependency failures counted once; mark affected domains
 * - Unmapped alerts never affect verdict
 */

import type { SystemDomainId } from '@/lib/architecture/systemDomainCatalog'
import { systemDomainLabel } from '@/lib/architecture/systemDomainCatalog'
import {
  applyRemediationToTriage,
  attentionCtaActionLabel,
  classifyAttentionAlert,
  classifyAttentionSignal,
} from './attentionRemediationCatalog'
import { verdictAffectingAlerts } from './alertMapping'
import { OBSERVABILITY_DOMAIN_ORDER, SIGNAL_STALE_MS } from './signalRegistry'
import type {
  AttentionItem,
  DomainHealth,
  DomainProbeability,
  EvaluatedSignal,
  GapSummary,
  MappedAlert,
  ObservabilityEnvId,
  ObservabilityVerdict,
  SignalDef,
  SignalGap,
  SignalState,
  SystemVerdict,
} from './types'
import { VERDICT_LABELS } from './types'

/**
 * Owner decision (product direction 1, 2026-08): Mission Control / Governance
 * keep Apollo seven-domain taxonomy membership but have no inventable runtime
 * probe — demote from Observability health-grid / gap primary narrative.
 * Allowlist is a safety fallback if registry rows drift.
 */
const REFERENCE_DOMAIN_ALLOWLIST: ReadonlySet<SystemDomainId> = new Set([
  'mission-control',
  'governance',
])

/**
 * Probeability SSOT: data-driven when every required signal is
 * `optionalContract` + `source: 'none'`; else Owner allowlist fallback.
 */
export function domainProbeability(
  domain: SystemDomainId,
  signals: Array<EvaluatedSignal | SignalDef>,
): DomainProbeability {
  const required: SignalDef[] = signals.map(s => ('def' in s ? s.def : s)).filter(d => d.role === 'required')
  if (
    required.length > 0 &&
    required.every(d => d.optionalContract === true && d.source === 'none')
  ) {
    return 'reference'
  }
  if (
    REFERENCE_DOMAIN_ALLOWLIST.has(domain) &&
    (required.length === 0 || required.every(d => d.optionalContract === true))
  ) {
    return 'reference'
  }
  return 'runtime'
}

export function isReferenceDomain(domain: DomainHealth): boolean {
  return domain.probeability === 'reference'
}

const VERDICT_RANK: Record<ObservabilityVerdict, number> = {
  critical: 5,
  degraded: 4,
  unknown: 3,
  not_observed: 2,
  healthy: 1,
}

export function maxVerdict(a: ObservabilityVerdict, b: ObservabilityVerdict): ObservabilityVerdict {
  return VERDICT_RANK[a] >= VERDICT_RANK[b] ? a : b
}

export function signalStateToVerdict(state: SignalState): ObservabilityVerdict {
  switch (state) {
    case 'healthy':
    case 'expected_off':
      return 'healthy'
    case 'degraded':
      return 'degraded'
    case 'critical':
      return 'critical'
    case 'not_observed':
      return 'not_observed'
    default:
      return 'unknown'
  }
}

/**
 * Map an evaluated signal to Expected vs Actual gap.
 * - healthy / expected_off → ok
 * - critical / degraded → fail
 * - unknown → blind
 * - not_observed + optionalContract → by_design
 * - not_observed + !optionalContract → blind
 */
export function signalToGap(s: EvaluatedSignal): SignalGap {
  switch (s.state) {
    case 'healthy':
    case 'expected_off':
      return 'ok'
    case 'critical':
    case 'degraded':
      return 'fail'
    case 'not_observed':
      return s.def.optionalContract === true ? 'by_design' : 'blind'
    default:
      return 'blind'
  }
}

export function emptyGapSummary(): GapSummary {
  return { ok: 0, fail: 0, blind: 0, byDesign: 0, total: 0 }
}

/** Aggregate gap counts for required signals only. */
export function gapSummaryFromSignals(signals: EvaluatedSignal[]): GapSummary {
  const summary = emptyGapSummary()
  for (const s of signals) {
    if (s.def.role !== 'required') continue
    summary.total += 1
    const gap = signalToGap(s)
    if (gap === 'ok') summary.ok += 1
    else if (gap === 'fail') summary.fail += 1
    else if (gap === 'blind') summary.blind += 1
    else summary.byDesign += 1
  }
  return summary
}

/**
 * Sum gapSummary across domains (system-level rollup).
 * Default excludes pure reference domains so MC/Governance by-design rows
 * do not inflate fail/blind/ok primary meta. Pass `includeReference: true`
 * to include them (e.g. catalog diagnostics).
 */
export function sumGapSummaries(
  domains: DomainHealth[],
  opts?: { includeReference?: boolean },
): GapSummary {
  const summary = emptyGapSummary()
  for (const d of domains) {
    if (!opts?.includeReference && d.probeability === 'reference') continue
    summary.ok += d.gapSummary.ok
    summary.fail += d.gapSummary.fail
    summary.blind += d.gapSummary.blind
    summary.byDesign += d.gapSummary.byDesign
    summary.total += d.gapSummary.total
  }
  return summary
}

/**
 * Roll up required signals for one domain.
 * Evidence-only signals are ignored for the verdict.
 */
export function domainVerdictFromSignals(
  signals: EvaluatedSignal[],
  domainAlerts: MappedAlert[],
): { verdict: ObservabilityVerdict; reason: string } {
  const required = signals.filter(s => s.def.role === 'required')
  if (required.length === 0) {
    return { verdict: 'not_observed', reason: 'No required signals registered for this domain' }
  }

  // Neutral states: expected_off (policy) and optional-contract not_observed
  // (no reliable runtime contract yet — must not pin the domain to UNKNOWN).
  const scored = required.filter(
    s =>
      s.state !== 'expected_off' &&
      !(s.state === 'not_observed' && s.def.optionalContract === true),
  )
  if (scored.length === 0) {
    return {
      verdict: 'not_observed',
      reason: 'No reliable runtime contract for required signals',
    }
  }
  const critical = scored.filter(s => s.state === 'critical')
  const degraded = scored.filter(s => s.state === 'degraded')
  const unknown = scored.filter(s => s.state === 'unknown')
  const notObs = scored.filter(s => s.state === 'not_observed')

  const affecting = verdictAffectingAlerts(domainAlerts)
  const critAlerts = affecting.filter(a => a.severity === 'critical')
  const warnAlerts = affecting.filter(a => a.severity === 'warning')

  if (critical.length > 0 || critAlerts.length > 0) {
    const cause =
      critical.length > 0
        ? critical.map(s => s.def.label).join(', ')
        : critAlerts.map(a => a.name).join(', ')
    return { verdict: 'critical', reason: `Critical: ${cause}` }
  }
  if (degraded.length > 0 || warnAlerts.length > 0) {
    const cause =
      degraded.length > 0
        ? degraded.map(s => s.def.label).join(', ')
        : warnAlerts.map(a => a.name).join(', ')
    return { verdict: 'degraded', reason: `Degraded: ${cause}` }
  }
  if (unknown.length > 0) {
    return {
      verdict: 'unknown',
      reason: `Probe missing/stale: ${unknown.map(s => s.def.label).join(', ')}`,
    }
  }
  if (notObs.length === scored.length) {
    return {
      verdict: 'not_observed',
      reason: 'No reliable runtime contract for required signals',
    }
  }
  if (notObs.length > 0) {
    // Partial observation without failure → UNKNOWN (do not fake HEALTHY).
    return {
      verdict: 'unknown',
      reason: `Partially observed — missing: ${notObs.map(s => s.def.label).join(', ')}`,
    }
  }
  const expectedOff = required.filter(s => s.state === 'expected_off').length
  const optionalNotObserved = required.filter(
    s => s.state === 'not_observed' && s.def.optionalContract === true,
  ).length
  const notes = [
    expectedOff > 0 ? `${expectedOff} expected off` : null,
    optionalNotObserved > 0 ? `${optionalNotObserved} not observed (optional)` : null,
  ].filter(n => n != null)
  return {
    verdict: 'healthy',
    reason:
      notes.length > 0
        ? `All scored required signals healthy · ${notes.join(' · ')}`
        : 'All required signals healthy',
  }
}

export function buildDomainHealth(
  domain: SystemDomainId,
  signals: EvaluatedSignal[],
  alerts: MappedAlert[],
): DomainHealth {
  const domainAlerts = alerts.filter(a => a.domain === domain)
  const { verdict, reason } = domainVerdictFromSignals(signals, domainAlerts)
  const required = signals.filter(s => s.def.role === 'required')
  const evidence = signals.filter(s => s.def.role === 'evidence')
  const observed = required.filter(
    s => s.state !== 'not_observed' && s.state !== 'unknown',
  ).length

  const scopes = new Set(signals.map(s => s.def.scope))
  let envScope: DomainHealth['envScope'] = 'none'
  if (scopes.has('env') && scopes.has('shared')) envScope = 'mixed'
  else if (scopes.has('shared')) envScope = 'shared'
  else if (scopes.has('env')) envScope = 'env'

  const sharedDependencyIds = [
    ...new Set(
      signals
        .filter(s => s.def.scope === 'shared' && s.def.role === 'required')
        .map(s => s.def.id),
    ),
  ]

  return {
    domain,
    label: systemDomainLabel(domain),
    verdict,
    reason,
    coverage: {
      observed,
      required: required.length,
      evidence: evidence.length,
    },
    gapSummary: gapSummaryFromSignals(signals),
    probeability: domainProbeability(domain, signals),
    alertCount: domainAlerts.filter(a => a.state === 'firing' || a.state === 'pending').length,
    envScope,
    signals,
    sharedDependencyIds,
  }
}

export function buildSystemVerdict(
  domains: DomainHealth[],
  alerts: MappedAlert[],
  opts: {
    env: ObservabilityEnvId
    generatedAt: string
    freshnessMs: number | null
  },
): SystemVerdict {
  const runtimeDomains = domains.filter(d => d.probeability === 'runtime')
  const referenceDomainCount = domains.filter(d => d.probeability === 'reference').length

  // domainCounts = runtime only — reference NOT OBSERVED must not dilute the grid narrative.
  const domainCounts: Record<ObservabilityVerdict, number> = {
    healthy: 0,
    degraded: 0,
    critical: 0,
    unknown: 0,
    not_observed: 0,
  }
  for (const d of runtimeDomains) {
    domainCounts[d.verdict] += 1
  }

  // Overall ignores pure NOT OBSERVED runtime domains unless every runtime domain is not_observed.
  const participating = runtimeDomains.filter(d => d.verdict !== 'not_observed')
  let overall: ObservabilityVerdict = 'healthy'
  let primaryCause = 'All observed domains healthy'

  if (runtimeDomains.length === 0) {
    overall = 'not_observed'
    primaryCause = 'No domains with reliable runtime contracts'
  } else if (participating.length === 0) {
    overall = 'not_observed'
    primaryCause = 'No domains with reliable runtime contracts'
  } else {
    for (const d of participating) {
      overall = maxVerdict(overall, d.verdict)
    }
    if (overall !== 'healthy') {
      const worst = participating
        .filter(d => d.verdict === overall)
        .map(d => `${d.label}: ${d.reason}`)
      primaryCause = worst[0] ?? primaryCause
    }
  }

  const affecting = verdictAffectingAlerts(alerts)
  const firing = alerts.filter(a => a.state === 'firing' || a.state === 'pending')

  const stale =
    opts.freshnessMs == null ? true : opts.freshnessMs > SIGNAL_STALE_MS

  return {
    overall,
    label: VERDICT_LABELS[overall],
    domainCounts,
    referenceDomainCount,
    firingAlerts: firing.length,
    mappedFiringAlerts: affecting.length,
    primaryCause,
    env: opts.env,
    freshnessMs: opts.freshnessMs,
    stale,
    generatedAt: opts.generatedAt,
  }
}

export function buildAttentionItems(
  domains: DomainHealth[],
  alerts: MappedAlert[],
  opts: {
    ownerByDomain?: Partial<Record<SystemDomainId, string>>
    grafanaUrlFor?: (item: {
      domain: SystemDomainId
      signalId: string
      env: ObservabilityEnvId
      activeAt?: string
    }) => string | null
  } = {},
): AttentionItem[] {
  const items: AttentionItem[] = []
  const ownerByDomain: Partial<Record<SystemDomainId, string>> = {
    rocket: 'Rocket / Cluster',
    'ground-systems': 'Ground Systems',
    satellite: 'Satellite',
    subcontractors: 'Subcontractors / IB Gateway',
    engineer: 'Engineer',
    'mission-control': 'Mission Control',
    governance: 'Governance',
    ...opts.ownerByDomain,
  }

  for (const d of domains) {
    for (const s of d.signals) {
      if (s.def.role !== 'required') continue
      // Optional-contract NOT OBSERVED is neutral — skip attention noise.
      const nonOptionalNotObserved =
        s.state === 'not_observed' && s.def.optionalContract !== true
      if (
        s.state !== 'critical' &&
        s.state !== 'degraded' &&
        s.state !== 'unknown' &&
        !nonOptionalNotObserved
      ) {
        continue
      }

      const severity: AttentionItem['severity'] =
        s.state === 'critical' ? 'critical' : s.state === 'degraded' ? 'warning' : 'info'
      const affected = s.def.affectsDomains ?? [s.def.domain]
      const remediation = classifyAttentionSignal({
        signalId: s.def.id,
        signalLabel: s.def.label,
        domain: s.def.domain,
        env: s.env,
        summary: s.summary,
        detailRoute: s.def.detailRoute,
      })
      const baseTriage = {
        whatHappened: s.summary,
        whyVerdictChanged: `${d.label} verdict is ${VERDICT_LABELS[d.verdict]} because required signal "${s.def.label}" is ${s.state.replace('_', ' ').toUpperCase()}`,
        affectedDomains: affected,
        evidence: s.evidence ?? s.summary,
        recommendedDestination: s.def.detailRoute ?? 'observability',
        detailRoute: s.def.detailRoute,
        grafanaUrl:
          opts.grafanaUrlFor?.({
            domain: s.def.domain,
            signalId: s.def.id,
            env: s.env,
          }) ?? null,
        track: remediation.track,
        playbookId: remediation.playbookId,
        cta: remediation.cta,
        trackReason: remediation.trackReason,
        suggestedAction: remediation.suggestedAction,
      }
      items.push({
        id: `signal:${s.def.id}:${s.env}`,
        severity,
        domain: s.def.domain,
        env: s.env,
        signalId: s.def.id,
        signalLabel: s.def.label,
        owner: ownerByDomain[s.def.domain] ?? s.def.domain,
        action: attentionCtaActionLabel(remediation.cta),
        summary: s.summary,
        triage: applyRemediationToTriage(baseTriage, remediation),
      })
    }
  }

  for (const a of verdictAffectingAlerts(alerts)) {
    if (a.domain == null) continue
    const domainHealth = domains.find(d => d.domain === a.domain)
    const remediation = classifyAttentionAlert(a)
    const baseTriage = {
      whatHappened: a.summary,
      whyVerdictChanged: `${systemDomainLabel(a.domain)} includes mapped ${a.severity} alert "${a.name}"`,
      affectedDomains: [a.domain],
      evidence: `state=${a.state}; labels=${JSON.stringify(a.labels)}`,
      recommendedDestination: 'observability',
      detailRoute: domainHealth?.signals[0]?.def.detailRoute,
      grafanaUrl:
        opts.grafanaUrlFor?.({
          domain: a.domain,
          signalId: `alert.${a.name}`,
          env: a.env,
          activeAt: a.activeAt,
        }) ?? null,
      track: remediation.track,
      playbookId: remediation.playbookId,
      cta: remediation.cta,
      trackReason: remediation.trackReason,
      suggestedAction: remediation.suggestedAction,
    }
    items.push({
      id: `alert:${a.id}`,
      severity: a.severity,
      domain: a.domain,
      env: a.env,
      signalId: `alert.${a.name}`,
      signalLabel: a.name,
      since: a.activeAt,
      owner: ownerByDomain[a.domain] ?? a.domain,
      action: attentionCtaActionLabel(remediation.cta),
      summary: a.summary,
      triage: applyRemediationToTriage(baseTriage, remediation),
    })
  }

  const rank = { critical: 0, warning: 1, info: 2, unknown: 3 }
  return items.sort((a, b) => rank[a.severity] - rank[b.severity])
}

export function emptyDomainCounts(): Record<ObservabilityVerdict, number> {
  return { healthy: 0, degraded: 0, critical: 0, unknown: 0, not_observed: 0 }
}

export { OBSERVABILITY_DOMAIN_ORDER, SIGNAL_STALE_MS }
