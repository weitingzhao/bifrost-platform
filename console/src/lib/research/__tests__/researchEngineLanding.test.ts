import { afterEach, describe, expect, it } from 'vitest'
import { buildResearchEngineLlmPack, RESEARCH_DBT_MODEL_COUNT } from '@/lib/architecture/researchEngineCatalog'
import {
  consumeResearchEngineLandingTab,
  setResearchEngineLandingTab,
} from '@/lib/research/researchEngineLanding'

describe('researchEngineLanding', () => {
  afterEach(() => {
    sessionStorage.removeItem('research-engine-landing-tab')
  })

  it('defaults to pipeline health when no landing tab is set', () => {
    expect(consumeResearchEngineLandingTab()).toBe('health')
  })

  it('consumes catalog once for retired Analytics hash', () => {
    setResearchEngineLandingTab('catalog')
    expect(consumeResearchEngineLandingTab()).toBe('catalog')
    expect(consumeResearchEngineLandingTab()).toBe('health')
  })
})

describe('researchEngineCatalog dbt absorb', () => {
  it('keeps the 21-model SEPA inventory and marks Analytics retired', () => {
    expect(RESEARCH_DBT_MODEL_COUNT).toBe(21)
    const pack = buildResearchEngineLlmPack()
    expect(pack).toMatch(/Plugin → Analytics/)
    expect(pack).toMatch(/dbt \/ Lineage/)
    expect(pack).not.toMatch(/Plugin → Research \(tab/)
    expect(pack).toMatch(/Satellite → Research Engine/)
    expect(pack).toMatch(/\/api\/v1\/research\/analytics\/elementary\/files\/elementary_report\.html/)
    expect(pack).not.toMatch(/\/plugins\/analytics\/api\/elementary_report/)
  })
})
