import { describe, expect, it } from 'vitest'
import { isMdDebugProbeEnabled } from '@/components/market-data/quality/mdNavParams'

describe('isMdDebugProbeEnabled', () => {
  it('is off by default', () => {
    expect(isMdDebugProbeEnabled('')).toBe(false)
    expect(isMdDebugProbeEnabled('?tab=ingest')).toBe(false)
  })

  it('accepts debug=1 / true / yes', () => {
    expect(isMdDebugProbeEnabled('?debug=1')).toBe(true)
    expect(isMdDebugProbeEnabled('?tab=ingest&debug=true')).toBe(true)
    expect(isMdDebugProbeEnabled('?debug=YES')).toBe(true)
  })

  it('rejects other debug values', () => {
    expect(isMdDebugProbeEnabled('?debug=0')).toBe(false)
    expect(isMdDebugProbeEnabled('?debug=false')).toBe(false)
  })
})
