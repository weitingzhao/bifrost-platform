import { describe, expect, it } from 'vitest'
import { researchRunLogHint } from '@/lib/delivery/pipelineRunAskPack'

describe('researchRunLogHint', () => {
  it('names the Argo apply timeout from rollout-research', () => {
    const hint = researchRunLogHint(
      'ERROR: deployment spec still x:0.1.0 after 300s.\nArgo CD has not applied the manifest pinning 0.2.0.',
    )
    expect(hint.tone).toBe('warning')
    expect(hint.message).toMatch(/Argo CD/)
  })

  it('reads the pre pin-check verify failure as informational', () => {
    const hint = researchRunLogHint('The image was pushed, but k8s/api/deployment.yaml still points')
    expect(hint.tone).toBe('info')
  })

  it('treats a pinned-run tag mismatch as a real failure', () => {
    const hint = researchRunLogHint('ERROR: deployment is not running the tag this run built.')
    expect(hint.tone).toBe('warning')
    expect(hint.message).toMatch(/manifest pins this tag/)
  })

  it('falls back to a real-failure warning', () => {
    expect(researchRunLogHint(undefined).tone).toBe('warning')
  })
})
