import type { AttentionRemediationClass, ClassifyInput } from '@/domains/foundation/observability/attentionRemediationCatalog'
export function classifyTenantAttention(input: ClassifyInput): AttentionRemediationClass | undefined {
  const name = input.name
  const signalId = input.signalId
  const domain = input.domain
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
      suggestedAction: `Open ${route}; triage bus/matrix targets`,
    }
  }

  if (signalId.startsWith('subcontractors.') || domain === 'subcontractors') {
    return {
      track: 'product',
      cta: 'manual',
      trackReason: 'IB Gateway / subcontractor — plugin plane, fix it there',
      suggestedAction: 'Open Plugin Gallery / IB Gateway; fix connectivity there',
    }
  }

  return undefined
}
