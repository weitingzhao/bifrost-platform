/**
 * Observability Attention → assisted remediation classification.
 *
 * Attention stays a triage entry — classify + CTA only.
 * Execution reuses Cluster/Daily Ops: startRemediation + ambient Operator Dock.
 * No auto-remediate; no Prometheus silence/ack as "fixed".
 *
 * Playbook IDs align with clusterFailureTriage where names already exist.
 */

import type { SystemDomainId } from '@/lib/architecture/systemDomainCatalog'
import type { RemediationTrack } from '@/lib/cluster/clusterFailureTriage'
import { scopeForPlaybookId } from '@/lib/agent/playbookAgentPrompts'
import type {
  AttentionItem,
  AttentionRemediationCta,
  MappedAlert,
  ObservabilityEnvId,
} from './types'

export type { AttentionRemediationCta }

export type AttentionRemediationClass = {
  track: RemediationTrack
  playbookId?: string
  cta: AttentionRemediationCta
  trackReason: string
  suggestedAction: string
}

/** Fallback remediation scope when playbook has no dedicated runner scope. */
export const ATTENTION_DEFAULT_REMEDIATION_SCOPE = 'cluster_issues_full_auto'

type ClassifyInput = {
  /** Attention id prefix hint: `alert:…` vs `signal:…` */
  kind: 'alert' | 'signal'
  /** Alert name or signal label (human). */
  name: string
  /** Signal registry id or `alert.{name}`. */
  signalId: string
  domain: SystemDomainId
  env: ObservabilityEnvId
  summary: string
  labels?: Record<string, string>
  detailRoute?: string
}

function isTargetDownNodeScrape(name: string, labels?: Record<string, string>, summary?: string): boolean {
  if (!/^TargetDown$/i.test(name)) return false
  const job = (labels?.job ?? labels?.scrape_job ?? '').toLowerCase()
  const blob = `${job} ${labels?.instance ?? ''} ${summary ?? ''}`
  return /node-exporter|node_exporter|kubelet/i.test(blob)
}

/**
 * Classify an Attention signal/alert into remediation track + CTA.
 * standbyNeutral alerts are already excluded from Attention — never called for those.
 */
export function classifyAttentionItem(input: ClassifyInput): AttentionRemediationClass {
  const name = input.name
  const signalId = input.signalId
  const domain = input.domain
  const labels = input.labels ?? {}

  // ── Alert-name rules (Prometheus) ──
  if (/^KubeDaemonSetRolloutStuck$/i.test(name) || /^KubeDaemonSetMisScheduled$/i.test(name)) {
    return {
      track: 'agent-adhoc',
      playbookId: 'pod-failure-triage',
      cta: 'agent_fix',
      trackReason: 'DaemonSet stuck / mis-scheduled — node or scheduling issue (standby noise excluded)',
      suggestedAction:
        'Agent Fix: describe DaemonSet + node events; recover node or reschedule pods; verify Desired=Current',
    }
  }

  if (/^KubePodNotReady$/i.test(name)) {
    return {
      track: 'agent-adhoc',
      playbookId: 'pod-failure-triage',
      cta: 'agent_fix',
      trackReason: 'Pod NotReady — case-by-case diagnosis (standby noise excluded)',
      suggestedAction:
        'Agent Fix with namespace/workload scope; collect events/logs; escalate if same pod >24h',
    }
  }

  if (/^KubeNodeNotReady$/i.test(name) || /^KubeNodeUnreachable$/i.test(name)) {
    const node = labels.node ?? labels.nodename ?? labels.kubernetes_node ?? ''
    const elasticHint = /gpu|elastic|warehouse|ollama|minio/i.test(`${node} ${input.summary}`)
    return {
      track: 'playbook',
      playbookId: elasticHint ? 'elastic-node-recover' : 'core-node-recover',
      cta: 'agent_fix',
      trackReason: elasticHint
        ? 'Elastic / compute node NotReady — WOL / k3s-agent recover'
        : 'Core node NotReady — drain/evacuate + recover agent',
      suggestedAction: elasticHint
        ? 'Wake elastic node or restart k3s-agent; uncordon when Ready'
        : 'Cordon → drain workloads → restart k3s on host → uncordon',
    }
  }

  if (isTargetDownNodeScrape(name, labels, input.summary)) {
    return {
      track: 'agent-adhoc',
      playbookId: 'pod-failure-triage',
      cta: 'diagnose',
      trackReason: 'node-exporter / kubelet scrape down — diagnose host reachability before restart',
      suggestedAction:
        'Diagnose: confirm node Ready + node-exporter DaemonSet; check firewall/port 9100; then recover scrape',
    }
  }

  if (/TargetDown|ScrapeFailed/i.test(name)) {
    return {
      track: 'agent-adhoc',
      playbookId: 'pod-failure-triage',
      cta: 'diagnose',
      trackReason: 'Prometheus scrape target down — diagnose job/instance before remediating',
      suggestedAction: 'Diagnose scrape job labels; fix target pod/ServiceMonitor; verify /targets',
    }
  }

  // ── Signal registry rules ──
  if (
    domain === 'satellite' ||
    signalId.startsWith('satellite.') ||
    /bus-health|matrix/i.test(signalId)
  ) {
    const route = input.detailRoute ?? 'satellite-bus'
    return {
      track: 'product',
      playbookId: signalId.includes('matrix') || /matrix/i.test(name) ? 'matrix-target-triage' : undefined,
      cta: 'manual',
      trackReason: 'Satellite bus/matrix — inspect on Satellite plane (not cluster node recycle)',
      suggestedAction: `Open ${route}; triage bus/matrix targets; Agent Fix from Satellite if needed`,
    }
  }

  if (signalId.startsWith('subcontractors.') || domain === 'subcontractors') {
    return {
      track: 'product',
      cta: 'manual',
      trackReason: 'IB Gateway / subcontractor — plugin plane, not Observability Agent Fix',
      suggestedAction: 'Open Plugin Gallery / IB Gateway; fix connectivity there',
    }
  }

  if (signalId.startsWith('ground.') || domain === 'ground-systems') {
    return {
      track: 'infra',
      playbookId: signalId.includes('postgres') ? 'postgres-ha-recover' : undefined,
      cta: 'diagnose',
      trackReason: 'Shared data plane — diagnose Redis/Postgres before broad cluster fix',
      suggestedAction: 'Diagnose data NS health; open Cluster postgres/redis panels',
    }
  }

  if (signalId.startsWith('rocket.') || domain === 'rocket') {
    if (signalId.includes('layer-b') || signalId.includes('prometheus')) {
      return {
        track: 'infra',
        cta: 'manual',
        trackReason: 'Observability fabric (Layer B) — install/recover via Cluster',
        suggestedAction: 'Open Cluster → ensure kube-prometheus-stack; then return to Observability',
      }
    }
    if (signalId.includes('scrape')) {
      return {
        track: 'agent-adhoc',
        playbookId: 'pod-failure-triage',
        cta: 'diagnose',
        trackReason: 'Scrape target health degraded — diagnose before Agent Fix',
        suggestedAction: 'Diagnose down targets; fix exporters / ServiceMonitors',
      }
    }
  }

  if (signalId.startsWith('engineer.') || domain === 'engineer') {
    return {
      track: 'infra',
      cta: 'manual',
      trackReason: 'Agent plane self-health — fix runner/bridge first',
      suggestedAction: 'Open Agent Desk / Operator Plane; restore remediation runner',
    }
  }

  // ── Default ──
  return {
    track: 'agent-adhoc',
    cta: 'diagnose',
    trackReason: 'Unmapped Attention signal — diagnose before remediating',
    suggestedAction: 'Diagnose evidence in Inspect sheet; escalate to Cluster Failure Triage if chronic',
  }
}

/** Classify from a MappedAlert (Attention alert rows). */
export function classifyAttentionAlert(alert: MappedAlert): AttentionRemediationClass {
  return classifyAttentionItem({
    kind: 'alert',
    name: alert.name,
    signalId: `alert.${alert.name}`,
    domain: alert.domain ?? 'rocket',
    env: alert.env,
    summary: alert.summary,
    labels: alert.labels,
  })
}

/** Classify from a required-signal Attention row. */
export function classifyAttentionSignal(opts: {
  signalId: string
  signalLabel: string
  domain: SystemDomainId
  env: ObservabilityEnvId
  summary: string
  detailRoute?: string
}): AttentionRemediationClass {
  return classifyAttentionItem({
    kind: 'signal',
    name: opts.signalLabel,
    signalId: opts.signalId,
    domain: opts.domain,
    env: opts.env,
    summary: opts.summary,
    detailRoute: opts.detailRoute,
  })
}

/** Remediationscope for Attention Agent Fix / Diagnose — prefer playbook scope map. */
export function scopeForAttentionRemediation(playbookId: string | undefined): string {
  return scopeForPlaybookId(playbookId) ?? ATTENTION_DEFAULT_REMEDIATION_SCOPE
}

/** Prompt body for ambient startRemediation from an Attention item. */
export function buildAttentionRemediationPrompt(item: AttentionItem): string {
  const t = item.triage
  const mode = t.cta === 'diagnose' ? 'Diagnose (assisted — do not auto-fix)' : 'Agent Fix (assisted)'
  return [
    `Observability Attention · ${mode}`,
    t.playbookId != null ? `Playbook: ${t.playbookId}` : 'Playbook: (adhoc)',
    '',
    `Signal: ${item.signalLabel}`,
    `Domain: ${item.domain} · Env: ${item.env}`,
    `Severity: ${item.severity}`,
    `Track: ${t.track} — ${t.trackReason}`,
    '',
    'Suggested action:',
    t.suggestedAction,
    '',
    'What happened:',
    t.whatHappened,
    '',
    'Why verdict changed:',
    t.whyVerdictChanged,
    '',
    'Evidence:',
    t.evidence,
    '',
    'Constraints:',
    '- Assisted only — propose steps; request operator approval for actuation',
    '- Do not silence/ack Prometheus alerts as "fixed"',
    '- D10: never place_order / arm daemon / enable live trading',
    '- Prefer Cluster / Satellite detail routes for manual follow-up',
  ].join('\n')
}

/** Short Action column label from CTA. */
export function attentionCtaActionLabel(cta: AttentionRemediationCta): string {
  switch (cta) {
    case 'agent_fix':
      return 'Agent Fix'
    case 'diagnose':
      return 'Diagnose'
    default:
      return 'Manual next'
  }
}

/** Merge classification into Attention triage fields. */
export function applyRemediationToTriage(
  triage: AttentionItem['triage'],
  cls: AttentionRemediationClass,
): AttentionItem['triage'] {
  return {
    ...triage,
    track: cls.track,
    playbookId: cls.playbookId,
    cta: cls.cta,
    trackReason: cls.trackReason,
    suggestedAction: cls.suggestedAction,
  }
}
