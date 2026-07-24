import { describe, expect, it } from 'vitest'
import { buildTradeDeployPrompt } from '@/lib/agent/tradeDeployAgentPrompt'

describe('buildTradeDeployPrompt', () => {
  it('defaults operator context to Control Room Launch Pad', () => {
    const prompt = buildTradeDeployPrompt({})
    expect(prompt).toContain('## Operator context (Control Room Launch Pad at task start)')
    expect(prompt).toContain('**Agent Deploy** on the Control Room Launch Pad')
    expect(prompt).toContain('Do NOT enable live trading')
  })

  it('uses Deploy Satellite page surface when operatorSurface is set', () => {
    const prompt = buildTradeDeployPrompt({ operatorSurface: 'Deploy Satellite page' })
    expect(prompt).toContain('## Operator context (Deploy Satellite page at task start)')
    expect(prompt).toContain('**AI Deploy** on the Deploy Satellite page')
    expect(prompt).not.toContain('Control Room Launch Pad at task start')
  })
})
