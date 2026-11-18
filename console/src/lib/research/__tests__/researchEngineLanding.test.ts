import { afterEach, describe, expect, it } from 'vitest'
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
