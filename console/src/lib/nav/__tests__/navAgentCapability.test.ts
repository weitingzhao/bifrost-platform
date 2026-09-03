import { describe, expect, it } from 'vitest'
import { ClipboardCopy, Sparkles } from 'lucide-react'
import {
  isNavAgentCapable,
  navAgentAskIcon,
  navAgentAskIdleTitle,
  navAgentNeedsAsk,
} from '@/lib/nav/navAgentCapability'

describe('navAgentCapability', () => {
  it('marks Massive / Flex / IB Client / Research Engine / Code Health / Control Room as pack-capable', () => {
    expect(isNavAgentCapable('market-data-manage')).toBe(true)
    expect(isNavAgentCapable('flex-query-manage')).toBe(true)
    expect(isNavAgentCapable('ib-gateway-manage')).toBe(true)
    expect(isNavAgentCapable('research-engine')).toBe(true)
    expect(isNavAgentCapable('code-health')).toBe(true)
    expect(isNavAgentCapable('control-room')).toBe(true)
    expect(isNavAgentCapable('plugin-gallery')).toBe(false)
  })

  it('raises Ask when the lamp is not green (diagnose pages)', () => {
    expect(navAgentNeedsAsk('ok')).toBe(false)
    expect(navAgentNeedsAsk('degraded')).toBe(true)
    expect(navAgentNeedsAsk('fail')).toBe(true)
    expect(navAgentNeedsAsk('unknown')).toBe(true)
    expect(navAgentNeedsAsk(null)).toBe(false)
  })

  it('Code Health Ask escalates only on OVER', () => {
    expect(navAgentNeedsAsk('ok', 'code-health')).toBe(false)
    expect(navAgentNeedsAsk('degraded', 'code-health')).toBe(false)
    expect(navAgentNeedsAsk('unknown', 'code-health')).toBe(false)
    expect(navAgentNeedsAsk('fail', 'code-health')).toBe(true)
  })

  it('Code Health uses ClipboardCopy for Generate Agent Pack', () => {
    expect(navAgentAskIcon('code-health')).toBe(ClipboardCopy)
    expect(navAgentAskIcon('market-data-manage')).toBe(Sparkles)
    expect(navAgentAskIdleTitle('code-health', false)).toContain('Generate Agent Pack')
  })
})
