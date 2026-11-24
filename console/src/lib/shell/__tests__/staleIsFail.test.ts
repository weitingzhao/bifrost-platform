import { describe, expect, it } from 'vitest'
import { buildResearchVerdictCopy } from '@/lib/research/researchHealthCopy'
import { copiesTile } from '@/pages/shell/owner/dataTiles'

// Origin (2026-11-23, 箭10: Stale is older than its declared freshness, the design system's fail; Agents already said
// so and the city aligns). The Console's audit found it judged warn in five places: the Test copies tile, the IB slot
// with a stale feed, the Market Data probe and workers freshness tags, and Research's stale layer word.
describe('stale is fail', () => {
  it('the Test copies tile', () => {
    const tile = copiesTile({ data: { cluster_id: '', reference_db: 'bifrost_prod', cache_hit: false, generated_at: '', fresh_threshold_days: 3, stale_threshold_days: 7,
      databases: [{ name: 'bifrost_dev', environment: 'dev', verdict: 'stale', lag_vs_prod_days: 9 }] }, error: null })
    expect(tile.tone).toBe('fail')
  })

  // 2026-11-23 箭10: Aging (past fresh, not yet past the declared freshness) is warn, not info.
  it('the Test copies tile, aging', () => {
    const tile = copiesTile({ data: { cluster_id: '', reference_db: 'bifrost_prod', cache_hit: false, generated_at: '', fresh_threshold_days: 3, stale_threshold_days: 7,
      databases: [{ name: 'bifrost_dev', environment: 'dev', verdict: 'aging', lag_vs_prod_days: 5 }] }, error: null })
    expect(tile).toMatchObject({ tone: 'warn', state: 'Aging' })
  })

  it("Research's stale layer reads as a failure", () => {
    // A stale feed lands on the degraded layer verdict, which ranks and lights as a failure; its word is fail too.
    const copy = buildResearchVerdictCopy({ loading: false, reachable: true, marketVerdict: 'stale', productOverall: 'degraded', batchVerdict: 'healthy' })
    expect(copy.layers.find(l => l.id === 'feedstock')?.verdict).toBe('degraded')
    expect(copy.tagLabel).toBe('fail')
  })
})
