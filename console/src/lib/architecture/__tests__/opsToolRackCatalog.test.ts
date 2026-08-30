import { describe, expect, it } from 'vitest'
import {
  OPS_TOOLS,
  OPS_TOOL_RACK_RULES,
  OPS_TOOL_RACK_VERSION,
  buildOpsToolRackLlmPack,
  opsToolById,
  resolveOpsToolUrl,
} from '@/lib/architecture/opsToolRackCatalog'

describe('opsToolRackCatalog', () => {
  it('exposes gitea, grafana, and dagster with LAN URLs', () => {
    expect(OPS_TOOLS.map(t => t.id).sort()).toEqual(['dagster', 'gitea', 'grafana'])
    for (const t of OPS_TOOLS) {
      expect(t.lanUrl).toMatch(/^https?:\/\//)
      expect(t.kind).toBe('external_ui')
      expect(t.purpose.length).toBeGreaterThan(0)
    }
  })

  it('forbids iframe embedding in rules', () => {
    expect(OPS_TOOL_RACK_RULES.some(r => /never iframe/i.test(r))).toBe(true)
  })

  it('resolveOpsToolUrl prefers live base then catalog', () => {
    expect(resolveOpsToolUrl('grafana', 'http://live.example:30883/')).toBe(
      'http://live.example:30883',
    )
    expect(resolveOpsToolUrl('grafana', null)).toBe(opsToolById('grafana').lanUrl)
    expect(resolveOpsToolUrl('dagster')).toBe('http://192.168.10.73:30301')
  })

  it('builds an LLM pack with version stamp', () => {
    const pack = buildOpsToolRackLlmPack()
    expect(pack).toContain(OPS_TOOL_RACK_VERSION)
    expect(pack).toContain('gitea')
    expect(pack).toContain('never iframe')
  })
})
