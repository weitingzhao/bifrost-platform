import { describe, expect, it } from 'vitest'
import { gatherNavAgentPack, isNavAgentCapable } from '@/lib/nav/navAgentCapability'

describe('navAgentCapability', () => {
  it('no tab ships an Ask-for-Agent pack', () => {
    for (const id of [
      'market-data-manage',
      'flex-query-manage',
      'ib-gateway-manage',
      'research-engine',
      'code-health',
      'control-room',
      'plugin-gallery',
    ]) {
      expect(isNavAgentCapable(id)).toBe(false)
    }
  })

  it('gather refuses every tab', async () => {
    await expect(gatherNavAgentPack('code-health')).rejects.toThrow('No Ask-for-Agent pack')
  })
})
