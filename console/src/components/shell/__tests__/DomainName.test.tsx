import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { DomainName } from '../DomainName'
import { DOMAIN_GLYPH } from '@/domains/owner/city/gen2/content/domainGlyphs'

// Origin (2026-11-23, World's consistency-gap table item 6): the Console's domain rows had no icon while the City and
// Signals drew one per domain from DOMAIN_GLYPH. The Console reads the same table.
describe('DomainName', () => {
  it('draws the shared table glyph beside the name', () => {
    const { container } = render(<DomainName id="delivery" />)
    expect(DOMAIN_GLYPH.delivery).toBe('rocket')
    expect(container.querySelector('svg')).not.toBeNull()
    expect(container.textContent).toBe('delivery')
  })

  it('a name the table does not know gets no glyph, not a made-up one', () => {
    const { container } = render(<DomainName id="not-a-domain" />)
    expect(container.querySelector('svg')).toBeNull()
  })
})
