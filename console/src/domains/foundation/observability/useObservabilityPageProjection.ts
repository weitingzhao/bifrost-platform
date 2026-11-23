import { VERDICT_LABELS } from '@/domains/foundation/observability/types'
import { useMemo } from 'react'
import type { SystemDomainId } from '@/lib/architecture/systemDomainCatalog'
import { filterMutedAttention, listActiveAttentionMutes, signalToGap, sumGapSummaries, type ObservabilityViewModel } from '@/domains/foundation/observability'
import { firingOffCards } from '@/domains/foundation/observability/verdictAggregation'
import { attentionMatchesScope, rollupDomainVerdict, type AttentionScopeFilter } from '@/domains/foundation/observability/components/observabilityFormat'

/** Complete page observation projection; no mute writes or operation authorization. */
export function useObservabilityPageProjection(viewModel: ObservabilityViewModel, selectedDomain: SystemDomainId, attentionScope: AttentionScopeFilter, muteRevision: number, isLoading: boolean) {
  const system = viewModel.system
  const selected = viewModel.selected
  const runtimeDomains = useMemo(
    () => viewModel.domains.filter(d => d.probeability === 'runtime'),
    [viewModel.domains],
  )
  /** Satellite (and any pure env-scoped domain) — follows Trade env selector. */
  const tradeEnvDomains = useMemo(
    () => runtimeDomains.filter(d => d.envScope === 'env'),
    [runtimeDomains],
  )
  /** Rocket / Ground / Subcontractors / Engineer — cluster fabric, not Trade-NS-scoped. */
  const sharedPlatformDomains = useMemo(
    () => runtimeDomains.filter(d => d.envScope !== 'env'),
    [runtimeDomains],
  )
  /** Unified Domain Health row — Trade-scoped first, then shared (no separate section). */
  const runtimeDomainCards = useMemo(
    () => [...tradeEnvDomains, ...sharedPlatformDomains],
    [tradeEnvDomains, sharedPlatformDomains],
  )
  const firingOffDomainCards = useMemo(
    () => firingOffCards(viewModel.alerts, new Set(runtimeDomainCards.map(d => d.domain))),
    [viewModel.alerts, runtimeDomainCards],
  )
  const referenceDomains = useMemo(
    () => viewModel.domains.filter(d => d.probeability === 'reference'),
    [viewModel.domains],
  )
  const tradeEnvRollup = useMemo(() => rollupDomainVerdict(tradeEnvDomains), [tradeEnvDomains])
  const sharedRollup = useMemo(
    () => rollupDomainVerdict(sharedPlatformDomains),
    [sharedPlatformDomains],
  )
  const filteredAttention = useMemo(() => {
    void muteRevision
    const scoped = viewModel.attention.filter(item => attentionMatchesScope(item, attentionScope))
    return filterMutedAttention(scoped)
  }, [viewModel.attention, attentionScope, muteRevision])

  const activeMuteCount = useMemo(() => {
    void muteRevision
    return listActiveAttentionMutes().length
  }, [muteRevision])

  const domainCountsLabel = useMemo(() => {
    const c = system.domainCounts
    // Domains counted under the same words their tags show (VERDICT_LABELS: the engine's seven).
    const parts = [
      c.critical > 0 ? `${c.critical} ${VERDICT_LABELS.critical}` : null,
      c.degraded > 0 ? `${c.degraded} ${VERDICT_LABELS.degraded}` : null,
      c.unknown > 0 ? `${c.unknown} ${VERDICT_LABELS.unknown}` : null,
      c.not_observed > 0 ? `${c.not_observed} ${VERDICT_LABELS.not_observed}` : null,
      c.healthy > 0 ? `${c.healthy} ${VERDICT_LABELS.healthy}` : null,
    ]
      .filter(Boolean)
      .join(' · ')
    return parts
  }, [system.domainCounts])

  /** Runtime domains only — reference planes excluded from primary gap meta. */
  const systemGapSummary = useMemo(
    () => sumGapSummaries(viewModel.domains),
    [viewModel.domains],
  )

  const selectedRequiredSignals = useMemo(() => {
    const domain = viewModel.domains.find(d => d.domain === selectedDomain)
    return (domain?.signals ?? []).filter(s => s.def.role === 'required')
  }, [viewModel.domains, selectedDomain])

  const selectedDomainHealthy = useMemo(() => {
    const d = viewModel.domains.find(x => x.domain === selectedDomain)
    return d?.verdict === 'healthy'
  }, [viewModel.domains, selectedDomain])

  const checkpointsQuiet =
    selectedRequiredSignals.length > 0 &&
    selectedRequiredSignals.every(s => {
      const gap = signalToGap(s)
      return gap === 'ok' || gap === 'by_design'
    })

  const dependencyQuiet =
    selected.dependencyPath.length > 0 &&
    selected.dependencyPath.every(
      hop => hop.state === 'healthy' || hop.state === 'expected_off',
    )

  const goldenQuiet =
    selected.goldenSignals.length > 0 &&
    selected.goldenSignals.every(g => g.status === 'ok')

  const scrapeQuiet = selected.scrapeRollup.quiet

  const systemHealthy = !isLoading && system.overall === 'healthy'
  const selectedPrimaryGrafana = useMemo(() => {
    const hit = selected.grafanaLinks.find(g => g.available && g.url != null)
    return hit?.url != null ? { label: hit.label, url: hit.url } : null
  }, [selected.grafanaLinks])
  const attentionQuiet =
    !isLoading && viewModel.attention.length === 0 && system.firingAlerts === 0

  return { runtimeDomainCards, firingOffDomainCards, referenceDomains, tradeEnvRollup, sharedRollup, filteredAttention, activeMuteCount, domainCountsLabel, systemGapSummary, selectedRequiredSignals, selectedDomainHealthy, checkpointsQuiet, dependencyQuiet, goldenQuiet, scrapeQuiet, systemHealthy, selectedPrimaryGrafana, attentionQuiet }
}
