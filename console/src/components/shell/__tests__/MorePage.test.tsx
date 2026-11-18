import '@testing-library/jest-dom/vitest'
import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { resolveTradeFrontendUrls } from '@/lib/tradeFrontendUrls'

// The real menu is a shared-UI popover; what More owns is the size it asks the trigger to be.
vi.mock('@/components/ToolsMenu', () => ({
  ToolsMenu: ({ className }: { className?: string }) => <button type="button" aria-label="Open Tools menu" className={className} />,
}))

const { MorePage } = await import('@/components/shell/MorePage')

afterEach(cleanup)

describe('More', () => {
  it('lists every detail page, the Trade app in each environment and the external tools', () => {
    render(<MorePage viewerEnv="prod" />)
    const details = within(screen.getByRole('navigation', { name: 'Detail pages' })).getAllByRole('link')
    expect(details).toHaveLength(8)
    const urls = resolveTradeFrontendUrls()
    const trade = within(screen.getByRole('navigation', { name: 'Trade' })).getAllByRole('link')
    expect(trade.map(a => a.getAttribute('href'))).toEqual([urls.dev, urls.stg, urls.prod])
    expect(screen.getByRole('link', { name: 'Open Trade · Prod (this seat)' })).toHaveAttribute('target', '_blank')
    // The side nav's footer is gone below 1440 px; More is where these live now (review 226 B-226-1, C-226-3).
    expect(screen.getByRole('button', { name: 'Open Tools menu' })).toHaveClass('h-11', 'w-11')
  })

  it('marks no environment as this seat while the seat is still being read', () => {
    render(<MorePage viewerEnv="prod" viewerEnvLoading />)
    expect(screen.queryByText('this seat')).toBeNull()
  })
})
