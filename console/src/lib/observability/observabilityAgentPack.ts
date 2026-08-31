/**
 * Clipboard / Agent Desk pack from Mission Control → Observability.
 * Mirrors Massive Copy for Agent — facts + interpretation, no secrets.
 *
 * HEALTHY required-signal verdict can coexist with firing alerts
 * (unmapped / info / standby-neutral). Those alerts must still appear.
 */

import { verdictAffectingAlerts } from '@/lib/observability/alertMapping'
import type {
  AttentionItem,
  MappedAlert,
  ObservabilityViewModel,
} from '@/lib/observability/types'
import { VERDICT_LABELS } from '@/lib/observability/types'

const MAX_ALERT_LINES = 60
const MAX_ATTENTION_LINES = 24

export type ObservabilityAgentPackContext = {
  generatedAt: string
  tradeEnv: string
  namespace: string
  selectedDomain: string
  viewModel: ObservabilityViewModel
}

export type ObservabilityPackFinding = {
  severity: 'warning' | 'info'
  title: string
  detail: string
}

function line(s: string): string {
  return s.replace(/\s+$/g, '')
}

function alertKind(alert: MappedAlert, affectingIds: Set<string>): string {
  if (alert.standbyNeutral) return 'standby_neutral'
  if (alert.expectedNeutral) return 'expected_neutral'
  if (affectingIds.has(alert.id)) return 'verdict_affecting'
  if (alert.severity === 'info') return 'info'
  if (!alert.mapped) return 'unmapped'
  return 'listed'
}

/** Strip CronJob scheduled-job suffix (`name-29779050`) so leftovers group. */
export function cronJobFamily(jobName: string | undefined): string {
  if (jobName == null || jobName === '') return ''
  return jobName.replace(/-\d{8,}$/, '')
}

function alertSubject(alert: MappedAlert): string {
  const l = alert.labels
  const family = cronJobFamily(l.job_name)
  if (family !== '') return `job=${family}`
  if (l.daemonset != null && l.daemonset !== '') return `ds=${l.daemonset}`
  if (l.pod != null && l.pod !== '') return `pod=${l.pod}`
  if (l.node != null && l.node !== '') return `node=${l.node}`
  if (l.instance != null && l.instance !== '') return `instance=${l.instance}`
  if (l.namespace != null && l.namespace !== '') return `ns=${l.namespace}`
  return 'instance=—'
}

type AlertPackGroup = { sample: MappedAlert; count: number }

function groupFiringAlerts(alerts: MappedAlert[], affectingIds: Set<string>): AlertPackGroup[] {
  const map = new Map<string, AlertPackGroup>()
  for (const a of alerts) {
    const kind = alertKind(a, affectingIds)
    const ns = a.labels.namespace ?? ''
    const family = cronJobFamily(a.labels.job_name)
    const key =
      family !== ''
        ? `${a.name}|${kind}|${ns}|${family}`
        : `${a.name}|${kind}|${ns}|${alertSubject(a)}`
    const existing = map.get(key)
    if (existing) existing.count += 1
    else map.set(key, { sample: a, count: 1 })
  }
  return [...map.values()]
}

function formatAlert(alert: MappedAlert, affectingIds: Set<string>, count = 1): string {
  const ns = alert.labels.namespace
  return (
    `- [${alert.severity}] ${alert.name}` +
    (count > 1 ? ` ×${count}` : '') +
    ` state=${alert.state}` +
    ` domain=${alert.domain ?? '—'} env=${alert.env}` +
    ` kind=${alertKind(alert, affectingIds)}` +
    ` mapped=${alert.mapped}` +
    (alert.standbyNeutral ? ' standby_neutral=true' : '') +
    (alert.expectedNeutral ? ' expected_neutral=true' : '') +
    (ns != null && ns !== '' ? ` ns=${ns}` : '') +
    ` ${alertSubject(alert)}` +
    (alert.activeAt ? ` since=${alert.activeAt}` : '') +
    ` · ${alert.summary}`
  )
}

function classifyFiring(alerts: MappedAlert[]) {
  const firing = alerts.filter(a => a.state === 'firing' || a.state === 'pending')
  const affecting = verdictAffectingAlerts(alerts)
  const standby = firing.filter(a => a.standbyNeutral)
  const expected = firing.filter(a => a.expectedNeutral && !a.standbyNeutral)
  const info = firing.filter(
    a => a.severity === 'info' && !a.standbyNeutral && !a.expectedNeutral,
  )
  const unmapped = firing.filter(
    a => !a.mapped && !a.standbyNeutral && !a.expectedNeutral && a.severity !== 'info',
  )
  return { firing, affecting, standby, expected, info, unmapped }
}

function formatAttention(item: AttentionItem): string {
  return (
    `- [${item.severity}] ${item.signalLabel} domain=${item.domain} env=${item.env}` +
    ` owner=${item.owner} cta=${item.triage.cta}` +
    (item.triage.playbookId ? ` playbook=${item.triage.playbookId}` : '') +
    ` · ${item.summary}`
  )
}

export function analyzeObservabilityPack(
  vm: ObservabilityViewModel,
): ObservabilityPackFinding[] {
  const findings: ObservabilityPackFinding[] = []
  const { firing, affecting, standby, expected, info, unmapped } = classifyFiring(vm.alerts)

  if (firing.length > 0) {
    findings.push({
      severity: vm.system.overall === 'healthy' ? 'warning' : 'info',
      title:
        vm.system.overall === 'healthy'
          ? 'HEALTHY verdict with firing alerts'
          : 'Firing alerts present',
      detail:
        `${firing.length} alert(s) firing/pending` +
        ` (${affecting.length} verdict-affecting · ${unmapped.length} unmapped` +
        ` · ${info.length} info · ${standby.length} standby-neutral · ${expected.length} expected-neutral).` +
        ' Include the Alerts section — do not treat a green required-signal verdict as “no Prometheus problems”.',
    })
  }
  if (vm.attention.length === 0 && firing.length > 0) {
    findings.push({
      severity: 'info',
      title: 'Attention empty while alerts fire',
      detail:
        'Attention only lists required-signal / verdict-affecting rows. Unmapped, info, and standby-neutral alerts stay on the domain cards and in this pack’s Alerts section.',
    })
  }
  for (const d of vm.domains) {
    if (d.probeability === 'runtime' && d.verdict !== 'healthy' && d.verdict !== 'not_observed') {
      findings.push({
        severity: 'warning',
        title: `${d.label} ${VERDICT_LABELS[d.verdict]}`,
        detail: d.reason,
      })
    }
  }
  const research = vm.domains.find(d => d.domain === 'research')
  if (research?.verdict === 'not_observed') {
    findings.push({
      severity: 'info',
      title: 'Research NOT OBSERVED',
      detail:
        'No required Observability signals for Research — diagnose Research Engine / Dagster on Satellite → Research Engine, not this hub.',
    })
  }
  return findings
}

export function buildObservabilityAgentPack(ctx: ObservabilityAgentPackContext): string {
  const { viewModel: vm, generatedAt, tradeEnv, namespace, selectedDomain } = ctx
  const lines: string[] = []
  const push = (...xs: string[]) => {
    for (const x of xs) lines.push(line(x))
  }

  const { firing, affecting, standby, expected, info, unmapped } = classifyFiring(vm.alerts)
  const affectingIds = new Set(affecting.map(a => a.id))
  const findings = analyzeObservabilityPack(vm)

  push(
    '# Observability — Agent repair pack',
    `Generated: ${generatedAt}`,
    'Source: Ops Console → Mission Control → Observability (Copy for Agent)',
    '',
    '## Goal',
    'Diagnose Observability system verdict, domain health, Attention rows, AND firing Prometheus alerts.',
    'HEALTHY required-signal verdict can coexist with firing alerts (unmapped / info / standby-neutral) — still list them.',
    'Constraints: D10 BLOCKED — no live trading / ib:operator:cmd / daemon scale-up.',
    'Do not invent runtime probes for Research or reference planes (Mission Control / Governance).',
    'Mute is not a root-cause fix.',
    '',
  )

  push(
    '## Console verdict',
    `SYSTEM VERDICT · ${tradeEnv.toUpperCase()}: ${vm.system.label}`,
    `primary_cause: ${vm.system.primaryCause}`,
    `env=${vm.system.env} ns=${namespace} selected_domain=${selectedDomain}`,
    `alerts firing=${vm.system.firingAlerts} mapped_affecting=${vm.system.mappedFiringAlerts}` +
      ` stale=${vm.system.stale} freshness_ms=${vm.system.freshnessMs ?? '—'}`,
    `Layer B: ${vm.layerBStatus}` +
      (vm.prometheusConfigured ? '' : ' · Prometheus not configured'),
    '',
  )

  push('## Domain health')
  for (const d of vm.domains) {
    const gap = d.gapSummary
    push(
      `- ${d.label} (${d.domain}): ${VERDICT_LABELS[d.verdict]} — ${d.reason}` +
        ` · required ${d.coverage.observed}/${d.coverage.required}` +
        ` · alerts=${d.alertCount}` +
        ` · probeability=${d.probeability}` +
        ` · gap ok=${gap.ok} fail=${gap.fail} blind=${gap.blind} by_design=${gap.byDesign}`,
    )
  }
  push('')

  push('## Findings')
  if (findings.length === 0) {
    push('(none — required signals healthy and no firing alerts)')
  } else {
    for (const f of findings) push(`- [${f.severity}] ${f.title}: ${f.detail}`)
  }
  push('')

  push('## Attention')
  if (vm.attention.length === 0) {
    push('(none — required signals clear for observed domains)')
  } else {
    for (const item of vm.attention.slice(0, MAX_ATTENTION_LINES)) {
      push(formatAttention(item))
    }
    if (vm.attention.length > MAX_ATTENTION_LINES) {
      push(`… ${vm.attention.length - MAX_ATTENTION_LINES} more Attention rows`)
    }
  }
  push('')

  push('## Alerts (Prometheus / Alertmanager)')
  push(
    'These are NOT the same as Attention. Domain HEALTHY + alertCount>0 is expected when alerts are unmapped, info, standby-neutral, or expected-neutral.',
    `firing_or_pending=${firing.length} verdict_affecting=${affecting.length}` +
      ` unmapped=${unmapped.length}` +
      ` info=${info.length}` +
      ` standby_neutral=${standby.length}` +
      ` expected_neutral=${expected.length}` +
      ` total_mapped_rows=${vm.alerts.length}`,
  )
  if (firing.length === 0) {
    push('(no firing/pending alerts)')
  } else {
    const groups = groupFiringAlerts(firing, affectingIds)
    const shown = groups.slice(0, MAX_ALERT_LINES)
    for (const g of shown) push(formatAlert(g.sample, affectingIds, g.count))
    if (groups.length > MAX_ALERT_LINES) {
      push(`… ${groups.length - MAX_ALERT_LINES} more firing alert groups omitted`)
    }
  }
  push('')

  const sel = vm.selected
  push(
    '## Selected domain',
    `${sel.domain} · checkpoints=${sel.dependencyPath.length} hops · scrape ${sel.scrapeRollup.label}`,
    `selected_alerts=${sel.alerts.length}`,
  )
  const downTargets = sel.scrapeTargets.filter(t => t.health === 'down' && t.expectedOff !== true)
  if (downTargets.length > 0) {
    push('unexpected scrape DOWN:')
    for (const t of downTargets.slice(0, 12)) {
      push(`  - ${t.job} ${t.instance}${t.lastError ? ` · ${t.lastError}` : ''}`)
    }
  }
  push('')

  push(
    '## Suggested investigation order',
    '1. If a runtime domain is DEGRADED/CRITICAL → start from Attention (required-signal fail). Use existing Agent Fix playbooks; do not invent a second execution engine.',
    '2. If SYSTEM HEALTHY but Alerts firing → classify each alert: verdict_affecting vs unmapped vs info vs standby_neutral vs expected_neutral. Do not mute as a fix.',
    '3. standby_neutral (elastic / WOL hosts) is expected-off — not a Rocket crash.',
    '3b. expected_neutral (k3s embedded control-plane *Down / version skew) is kube-prometheus stock vs k3s — not a Rocket crash.',
    '4. unmapped alerts are catalog/label gaps — they must not paint domain HEALTHY as a lie; fix mapping or Alertmanager labels.',
    '5. Research NOT OBSERVED → open Satellite → Research Engine (signal-health / Dagster). Do not add fake Observability probes.',
    '6. Missing scrape / Prometheus not configured → Rocket → Cluster Layer B. UNKNOWN ≠ HEALTHY.',
    '',
    '## Owner ask',
    'Propose the smallest durable fix, verify on Observability (verdict + Attention + Alerts), then report before/after.',
  )

  return lines.join('\n')
}

export function buildObservabilityDiagnosePrefill(ctx: ObservabilityAgentPackContext): string {
  const { viewModel: vm, tradeEnv } = ctx
  const findings = analyzeObservabilityPack(vm)
  const firing = vm.alerts.filter(a => a.state === 'firing' || a.state === 'pending')
  const affectingIds = new Set(verdictAffectingAlerts(vm.alerts).map(a => a.id))
  const lines = [
    'Observability — assisted diagnose (read-only). D10 BLOCKED — no live trading.',
    `Verdict: ${vm.system.label} · ${tradeEnv.toUpperCase()} — ${vm.system.primaryCause}`,
    `Alerts: ${firing.length} firing/pending · ${vm.system.mappedFiringAlerts} verdict-affecting · Attention ${vm.attention.length}`,
  ]
  if (findings.length > 0) {
    lines.push('', 'Findings:')
    for (const f of findings) lines.push(`- [${f.severity}] ${f.title}: ${f.detail}`)
  }
  if (firing.length > 0) {
    lines.push('', 'Firing alerts (include even when domain HEALTHY):')
    const groups = groupFiringAlerts(firing, affectingIds)
    for (const g of groups.slice(0, 20)) lines.push(formatAlert(g.sample, affectingIds, g.count))
    if (groups.length > 20) lines.push(`… ${groups.length - 20} more`)
  }
  lines.push(
    '',
    'Plan:',
    '1. Separate required-signal Attention from leftover Prometheus alerts.',
    '2. Classify standby-neutral vs expected-neutral vs info vs unmapped vs verdict-affecting.',
    '3. Do not mute as a fix. Do not invent Research probes.',
  )
  return lines.join('\n')
}
