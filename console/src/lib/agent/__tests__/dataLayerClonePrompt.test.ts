import { describe, expect, it } from 'vitest'
import { DATA_LAYER_CLONE_SCOPE } from '@/lib/agent/agentScopes'
import {
  buildDataLayerCloneOperatorPrompt,
  DATA_LAYER_CLONE_PLAYBOOK,
} from '@/lib/agent/dataLayerClonePrompt'

describe('buildDataLayerCloneOperatorPrompt', () => {
  it('marks the clone playbook and forbids STG/PROD', () => {
    const prompt = buildDataLayerCloneOperatorPrompt({
      lastCloneAt: '2026-08-13T17:49:26Z',
      lagDays: 0,
      verdict: 'fresh',
    })
    expect(prompt).toContain(DATA_LAYER_CLONE_PLAYBOOK)
    expect(prompt).toContain(`Scope: ${DATA_LAYER_CLONE_SCOPE}`)
    expect(prompt).toContain('bifrost_dev')
    expect(prompt).toContain('Do not touch bifrost_stg or bifrost_prod')
    expect(prompt).toContain('ConfirmDialog')
  })
})
