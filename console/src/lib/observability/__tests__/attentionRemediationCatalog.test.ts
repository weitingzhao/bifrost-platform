import { describe, expect, it } from 'vitest'
import {
  ATTENTION_DEFAULT_REMEDIATION_SCOPE,
  attentionCtaActionLabel,
  buildAttentionRemediationPrompt,
  classifyAttentionAlert,
  classifyAttentionItem,
  classifyAttentionSignal,
  scopeForAttentionRemediation,
} from '../attentionRemediationCatalog'
import { annotateStandbyAlerts, mapAlert, verdictAffectingAlerts } from '../alertMapping'
import { buildAttentionItems, buildDomainHealth } from '../verdictAggregation'
import type { AttentionItem, EvaluatedSignal } from '../types'
import { getSignalDef } from '../signalRegistry'

function sig(id: string, state: EvaluatedSignal['state']): EvaluatedSignal {
  const def = getSignalDef(id)!
  return {
    def,
    state,
    summary: `${def.label} is ${state}`,
    env: def.scope === 'env' ? 'stg' : 'shared',
  }
}

describe('classifyAttentionItem', () => {
  it('maps DaemonSet stuck to pod-failure-triage agent_fix', () => {
    const cls = classifyAttentionItem({
      kind: 'alert',
      name: 'KubeDaemonSetRolloutStuck',
      signalId: 'alert.KubeDaemonSetRolloutStuck',
      domain: 'rocket',
      env: 'shared',
      summary: 'DaemonSet kube-prometheus-stack-prometheus-node-exporter stuck',
      labels: { daemonset: 'node-exporter', node: 'ubt-k3s-01' },
    })
    expect(cls.cta).toBe('agent_fix')
    expect(cls.playbookId).toBe('pod-failure-triage')
    expect(cls.track).toBe('agent-adhoc')
  })

  it('maps KubeDaemonSetMisScheduled to same playbook', () => {
    const cls = classifyAttentionItem({
      kind: 'alert',
      name: 'KubeDaemonSetMisScheduled',
      signalId: 'alert.KubeDaemonSetMisScheduled',
      domain: 'rocket',
      env: 'shared',
      summary: 'mis-scheduled',
    })
    expect(cls.playbookId).toBe('pod-failure-triage')
    expect(cls.cta).toBe('agent_fix')
  })

  it('maps KubePodNotReady to pod-failure-triage agent_fix', () => {
    const cls = classifyAttentionItem({
      kind: 'alert',
      name: 'KubePodNotReady',
      signalId: 'alert.KubePodNotReady',
      domain: 'rocket',
      env: 'shared',
      summary: 'pod not ready',
      labels: { namespace: 'monitoring', pod: 'foo' },
    })
    expect(cls.cta).toBe('agent_fix')
    expect(cls.playbookId).toBe('pod-failure-triage')
  })

  it('maps core KubeNodeNotReady to core-node-recover', () => {
    const cls = classifyAttentionItem({
      kind: 'alert',
      name: 'KubeNodeNotReady',
      signalId: 'alert.KubeNodeNotReady',
      domain: 'rocket',
      env: 'shared',
      summary: 'ubt-k3s-01 NotReady',
      labels: { node: 'ubt-k3s-01' },
    })
    expect(cls.cta).toBe('agent_fix')
    expect(cls.playbookId).toBe('core-node-recover')
    expect(cls.track).toBe('playbook')
  })

  it('maps elastic/gpu NotReady to elastic-node-recover', () => {
    const cls = classifyAttentionItem({
      kind: 'alert',
      name: 'KubeNodeNotReady',
      signalId: 'alert.KubeNodeNotReady',
      domain: 'rocket',
      env: 'shared',
      summary: 'gpu-server NotReady — compute needed',
      labels: { node: 'gpu-server' },
    })
    expect(cls.playbookId).toBe('elastic-node-recover')
  })

  it('maps node-exporter TargetDown to diagnose', () => {
    const cls = classifyAttentionAlert(
      mapAlert(
        {
          labels: {
            alertname: 'TargetDown',
            severity: 'critical',
            job: 'node-exporter',
            instance: '192.168.10.10:9100',
          },
          state: 'firing',
        },
        0,
      ),
    )
    expect(cls.cta).toBe('diagnose')
    expect(cls.playbookId).toBe('pod-failure-triage')
  })

  it('maps satellite bus signal to manual', () => {
    const cls = classifyAttentionSignal({
      signalId: 'satellite.bus-health',
      signalLabel: 'Bus health (selected env)',
      domain: 'satellite',
      env: 'stg',
      summary: 'bus degraded',
      detailRoute: 'satellite-bus',
    })
    expect(cls.cta).toBe('manual')
    expect(cls.track).toBe('product')
  })

  it('maps unknown to diagnose', () => {
    const cls = classifyAttentionItem({
      kind: 'signal',
      name: 'Mystery',
      signalId: 'unknown.foo',
      domain: 'mission-control',
      env: 'shared',
      summary: '???',
    })
    expect(cls.cta).toBe('diagnose')
    expect(cls.playbookId).toBeUndefined()
  })
})

describe('scopeForAttentionRemediation', () => {
  it('falls back to cluster_issues_full_auto for adhoc playbooks', () => {
    expect(scopeForAttentionRemediation('pod-failure-triage')).toBe(
      ATTENTION_DEFAULT_REMEDIATION_SCOPE,
    )
    expect(scopeForAttentionRemediation(undefined)).toBe(ATTENTION_DEFAULT_REMEDIATION_SCOPE)
  })

  it('uses dedicated scope when playbook is mapped', () => {
    expect(scopeForAttentionRemediation('deliver-stg-recover')).toBe('deliver-stg-recover')
  })
})

describe('attentionCtaActionLabel', () => {
  it('labels CTAs for Action column', () => {
    expect(attentionCtaActionLabel('agent_fix')).toBe('Agent Fix')
    expect(attentionCtaActionLabel('diagnose')).toBe('Diagnose')
    expect(attentionCtaActionLabel('manual')).toBe('Manual next')
  })
})

describe('buildAttentionItems remediation wiring', () => {
  it('attaches remediation fields and excludes standbyNeutral', () => {
    const standby = [{ name: 'gpu-server', internalIp: '192.168.10.74' }]
    const standbyAlert = mapAlert(
      {
        labels: {
          alertname: 'KubeDaemonSetRolloutStuck',
          severity: 'warning',
          node: 'gpu-server',
        },
        state: 'firing',
      },
      0,
    )
    const coreAlert = mapAlert(
      {
        labels: {
          alertname: 'KubePodNotReady',
          severity: 'warning',
          namespace: 'monitoring',
          pod: 'alertmanager-0',
        },
        annotations: { summary: 'alertmanager not ready' },
        state: 'firing',
      },
      1,
    )
    const annotated = annotateStandbyAlerts([standbyAlert, coreAlert], standby)
    expect(verdictAffectingAlerts(annotated)).toHaveLength(1)

    const domains = [
      buildDomainHealth('rocket', [sig('rocket.scrape-targets', 'healthy')], annotated),
    ]
    const attention = buildAttentionItems(domains, annotated)
    expect(attention.some(a => a.signalLabel === 'KubeDaemonSetRolloutStuck')).toBe(false)

    const podItem = attention.find(a => a.signalLabel === 'KubePodNotReady')
    expect(podItem).toBeDefined()
    expect(podItem!.triage.cta).toBe('agent_fix')
    expect(podItem!.triage.playbookId).toBe('pod-failure-triage')
    expect(podItem!.triage.track).toBe('agent-adhoc')
    expect(podItem!.action).toBe('Agent Fix')
  })

  it('classifies degraded satellite bus signal as manual', () => {
    const domains = [
      buildDomainHealth('satellite', [sig('satellite.bus-health', 'degraded')], []),
    ]
    const attention = buildAttentionItems(domains, [])
    const bus = attention.find(a => a.signalId === 'satellite.bus-health')
    expect(bus).toBeDefined()
    expect(bus!.triage.cta).toBe('manual')
    expect(bus!.action).toBe('Manual next')
  })
})

describe('buildAttentionRemediationPrompt', () => {
  it('includes D10 and assisted constraints', () => {
    const item: AttentionItem = {
      id: 'alert:x',
      severity: 'warning',
      domain: 'rocket',
      env: 'shared',
      signalId: 'alert.KubePodNotReady',
      signalLabel: 'KubePodNotReady',
      owner: 'Rocket / Cluster',
      action: 'Agent Fix',
      summary: 'pod not ready',
      triage: {
        whatHappened: 'pod not ready',
        whyVerdictChanged: 'warning alert',
        affectedDomains: ['rocket'],
        evidence: 'state=firing',
        recommendedDestination: 'cluster',
        track: 'agent-adhoc',
        playbookId: 'pod-failure-triage',
        cta: 'agent_fix',
        trackReason: 'case-by-case',
        suggestedAction: 'collect logs',
      },
    }
    const prompt = buildAttentionRemediationPrompt(item)
    expect(prompt).toContain('Playbook: pod-failure-triage')
    expect(prompt).toContain('D10')
    expect(prompt).toContain('Assisted only')
    expect(prompt).toContain('never place_order')
  })
})
