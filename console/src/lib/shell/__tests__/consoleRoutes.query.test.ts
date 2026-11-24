// 2026-11-24 Engine's City 390 sweep, item 8: entering a building turned #city?at=delivery/gitops into
// at=delivery%2Fgitops. The shell's canonical rewrite dropped taskMode by passing the query through
// URLSearchParams.toString(), which re-encodes every pair. Now only the taskMode pair goes; the rest keep their spelling.
import { describe, expect, it } from 'vitest'
import { hashQueryWithoutTaskMode, resolveConsoleHash } from '@/lib/shell/consoleRoutes'

describe('the shell keeps an address as written', () => {
  it('leaves a query without taskMode exactly as it was', () => {
    expect(hashQueryWithoutTaskMode('city?at=delivery/gitops&sel=argo')).toBe('at=delivery/gitops&sel=argo')
  })
  it('drops only the taskMode pair', () => {
    expect(hashQueryWithoutTaskMode('status?taskMode=1&domain=plan')).toBe('domain=plan')
    expect(hashQueryWithoutTaskMode('status?domain=plan&taskMode=on')).toBe('domain=plan')
    expect(hashQueryWithoutTaskMode('status')).toBe('')
  })
  it('does not re-encode a City place in the canonical address', () => {
    expect(resolveConsoleHash('city?at=delivery/gitops').canonical).not.toContain('%2F')
  })
})
