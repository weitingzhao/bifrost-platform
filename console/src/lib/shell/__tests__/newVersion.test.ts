// 2026-11-23 箭10: the Owner's tablet keeps the Console open; after a deploy it kept the old bundle (UI / Console saw
// index-BtAdWEWW on PROD while the server served index-CMxX5eLY), so his view of our changes was simply old. The frame
// now says "New version · Reload" and an idle page reloads itself; a page in use does not, and the decision page keeps
// its own rule.
import { describe, expect, it } from 'vitest'
import { approvalHref } from '@/lib/shell/consoleRoutes'
import { onDecisionPage, shouldAutoReload } from '@/lib/shell/newVersion'

describe('new version', () => {
  it('reloads on its own only after five idle minutes with a newer build', () => {
    expect(shouldAutoReload(true, 0, 5 * 60_000)).toBe(true)
    expect(shouldAutoReload(true, 0, 4 * 60_000)).toBe(false) // touched four minutes ago: someone is using it
    expect(shouldAutoReload(false, 0, 60 * 60_000)).toBe(false) // no newer build: never
  })
  it('leaves the decision page to its own banner', () => {
    expect(onDecisionPage(approvalHref('r1'))).toBe(true)
    expect(onDecisionPage('#status')).toBe(false)
  })
})
