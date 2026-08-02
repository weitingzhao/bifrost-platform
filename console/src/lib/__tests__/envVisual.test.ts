import { describe, expect, it } from 'vitest'
import {
  isTradeEnvId,
  tradeEnvSegmentActiveClass,
  tradeEnvTagVariant,
  viewerSeatTagVariant,
} from '@/lib/envVisual'

describe('envVisual SSOT', () => {
  it('maps Trade NS / seat identity colors', () => {
    expect(tradeEnvTagVariant('dev')).toBe('info')
    expect(tradeEnvTagVariant('stg')).toBe('warning')
    expect(tradeEnvTagVariant('prod')).toBe('danger')
    expect(viewerSeatTagVariant('dev-local')).toBe('info')
    expect(viewerSeatTagVariant('prod')).toBe('danger')
  })

  it('exposes distinct active segment classes per env', () => {
    expect(isTradeEnvId('dev')).toBe(true)
    expect(isTradeEnvId('shared')).toBe(false)
    const classes = (['dev', 'stg', 'prod'] as const).map(tradeEnvSegmentActiveClass)
    expect(new Set(classes).size).toBe(3)
  })
})
