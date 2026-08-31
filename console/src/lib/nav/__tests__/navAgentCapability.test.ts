import { describe, expect, it } from 'vitest'
import {
  isNavAgentCapable,
  navAgentNeedsAsk,
} from '@/lib/nav/navAgentCapability'

describe('navAgentCapability', () => {
  it('marks Massive / Flex / Research Engine / Code Health as pack-capable', () => {
    expect(isNavAgentCapable('market-data-manage')).toBe(true)
    expect(isNavAgentCapable('flex-query-manage')).toBe(true)
    expect(isNavAgentCapable('research-engine')).toBe(true)
    expect(isNavAgentCapable('code-health')).toBe(true)
    expect(isNavAgentCapable('control-room')).toBe(false)
    expect(isNavAgentCapable('ib-gateway-manage')).toBe(false)
  })

  it('raises Ask when the lamp is not green', () => {
    expect(navAgentNeedsAsk('ok')).toBe(false)
    expect(navAgentNeedsAsk('degraded')).toBe(true)
    expect(navAgentNeedsAsk('fail')).toBe(true)
    expect(navAgentNeedsAsk('unknown')).toBe(true)
    expect(navAgentNeedsAsk(null)).toBe(false)
  })
})
