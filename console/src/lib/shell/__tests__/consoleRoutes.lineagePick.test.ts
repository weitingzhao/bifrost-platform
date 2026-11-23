import { describe, expect, it } from 'vitest'
import { lineagePick } from '@/lib/shell/consoleRoutes'
import { cityHref } from '@/domains/owner/city/gen2/content/looks'

// Origin (2026-11-23, UI / World asked): from Commit lineage with a line picked, the City link
// must land on that line; the City level's Lines card puts sel=<session> first.
describe('the line Commit lineage hands to the City', () => {
  it('is thread= on the commit-lineage section only', () => {
    expect(lineagePick('section=commit-lineage&thread=local_a')).toBe('local_a')
    expect(lineagePick('section=commit-lineage')).toBeNull()
    expect(lineagePick('section=history&thread=local_a')).toBeNull()
  })

  it('goes to the City level with sel', () => {
    expect(cityHref([], lineagePick('section=commit-lineage&thread=local_a'))).toBe('#city?sel=local_a')
  })
})
