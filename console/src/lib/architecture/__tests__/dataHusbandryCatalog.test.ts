import { describe, expect, it } from 'vitest'
import {
  DATA_HUSBANDRY_VERSION,
  HUSBANDRY_LANES,
  HUSBANDRY_RULES,
  RESEARCH_HEALTH_LAYERS,
  buildDataHusbandryLlmPack,
  husbandryLaneById,
} from '@/lib/architecture/dataHusbandryCatalog'
import {
  RESEARCH_ENGINE_SUMMARY,
  RESEARCH_GOVERNANCE_SURFACES,
  buildResearchEngineLlmPack,
} from '@/lib/architecture/researchEngineCatalog'

describe('dataHusbandryCatalog', () => {
  it('exposes three husbandry lanes', () => {
    expect(HUSBANDRY_LANES.map(l => l.id).sort()).toEqual([
      'flex_batch',
      'market_batch',
      'research_olap',
    ])
  })

  it('targets Dagster for batch lanes and forbids IB Client in graph', () => {
    for (const lane of HUSBANDRY_LANES) {
      expect(lane.schedulerTarget).toBe('dagster')
    }
    expect(HUSBANDRY_RULES.some(r => r.includes('IB Gateway'))).toBe(true)
    expect(HUSBANDRY_RULES.some(r => r.includes('fail-closed'))).toBe(true)
    expect(HUSBANDRY_RULES.some(r => r.includes('research_trading_day'))).toBe(true)
    expect(HUSBANDRY_RULES.some(r => /multi-schedule/i.test(r))).toBe(true)
    expect(HUSBANDRY_RULES.some(r => /dual-write/i.test(r))).toBe(true)
    // Outside graph: IB only — not Massive option-refresh / fundamentals Keep list.
    expect(HUSBANDRY_RULES.join('\n')).not.toMatch(/Keep: option-refresh/)
    expect(husbandryLaneById('flex_batch').mustNot).toMatch(/Enqueue without Flex/)
  })

  it('defines Research Engine three health layers; nav follows Product+Batch only', () => {
    expect(RESEARCH_HEALTH_LAYERS.map(l => l.id).sort()).toEqual([
      'batch',
      'feedstock',
      'product_asof',
    ])
    expect(RESEARCH_HEALTH_LAYERS.find(l => l.id === 'feedstock')?.navAffectsResearchIcon).toBe(
      false,
    )
    expect(RESEARCH_HEALTH_LAYERS.find(l => l.id === 'batch')?.navAffectsResearchIcon).toBe(true)
    expect(RESEARCH_HEALTH_LAYERS.find(l => l.id === 'product_asof')?.navAffectsResearchIcon).toBe(
      true,
    )
    expect(HUSBANDRY_RULES.some(r => /three layers/i.test(r))).toBe(true)
    expect(husbandryLaneById('research_olap').mustNot).toMatch(/Market missed/)
  })

  it('builds an LLM pack with version stamp and three-layer keywords', () => {
    const pack = buildDataHusbandryLlmPack()
    expect(pack).toContain(DATA_HUSBANDRY_VERSION)
    expect(pack).toContain('market_batch')
    expect(pack).toContain('void ≠ fail')
    expect(pack).toContain('feedstock')
    expect(pack).toContain('product_asof')
    expect(pack).toContain('Research Engine health layers')
  })
})

describe('researchEngineCatalog health surfaces', () => {
  it('exposes feedstock / batch / product-asof governance surfaces', () => {
    const ids = RESEARCH_GOVERNANCE_SURFACES.map(s => s.id)
    expect(ids).toContain('feedstock')
    expect(ids).toContain('batch')
    expect(ids).toContain('product-asof')
    expect(RESEARCH_ENGINE_SUMMARY.runtimeIgnition).toMatch(/Dagster/)
    expect(RESEARCH_ENGINE_SUMMARY.runtimeIgnition).not.toMatch(/replicas:0/)
  })

  it('LLM pack documents three-layer health and nav rule', () => {
    const pack = buildResearchEngineLlmPack()
    expect(pack).toContain('feedstock')
    expect(pack).toContain('product_asof')
    expect(pack).toContain('research_olap')
    expect(pack).toMatch(/sidebar Research Engine icon/i)
  })
})
