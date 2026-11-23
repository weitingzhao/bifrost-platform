import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { shellSelfHealthQuery } from '@/domains/foundation/shell/useShellStatusLine'
import { HarnessPreview } from '../HarnessPreview'

function renderPreview(viewerEnvironment: string, config: Response) {
  const fetchConfig = vi.fn().mockResolvedValue(config)
  vi.stubGlobal('fetch', fetchConfig)
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } })
  client.setQueryData(shellSelfHealthQuery.queryKey, { viewer_env: viewerEnvironment })
  render(<QueryClientProvider client={client}><HarnessPreview /></QueryClientProvider>)
  return fetchConfig
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('Ops Harness preview', () => {
  // 2026-11-22: the phone needs an HTTPS native origin; the Mac loopback preview only worked on the Mac.
  it('opens the configured production origin with a native new-tab fallback', async () => {
    const opencode = 'https://opencode-preview.ops.bifrost.lan/project/session'
    const deepseek = 'https://deepseek-preview.ops.bifrost.lan/'
    const fetchConfig = renderPreview('prod', Response.json({ opencode, deepseek }))
    fireEvent.click(await screen.findByRole('button', { name: 'OpenCode' }))
    expect(screen.getByTitle('OpenCode')).toHaveAttribute('src', opencode)
    expect(screen.getByRole('link', { name: 'Open OpenCode in a new tab' })).toHaveAttribute('href', opencode)
    fireEvent.click(screen.getByRole('button', { name: 'DeepSeek Harness' }))
    expect(screen.getByTitle('DeepSeek Harness')).toHaveAttribute('src', deepseek)
    expect(screen.queryByTitle('OpenCode')).toBeNull()
    expect(fetchConfig).toHaveBeenCalledWith('/runtime/harness-previews.json', expect.objectContaining({ cache: 'no-store' }))
  })

  it('keeps the entry hidden when an environment has no configured services', async () => {
    const fetchConfig = renderPreview('prod', new Response(null, { status: 404 }))
    await waitFor(() => expect(fetchConfig).toHaveBeenCalled())
    expect(screen.queryByRole('region', { name: 'Local harnesses' })).toBeNull()
  })

  it.each([
    window.location.origin + '/console',
    'https://deepseek-preview.ops.bifrost.lan/?token=should-not-be-public',
  ])('drops a preview URI with the wrong origin or public token (%s)', async deepseek => {
    renderPreview('prod', Response.json({ opencode: 'https://opencode-preview.ops.bifrost.lan/', deepseek }))
    await screen.findByRole('button', { name: 'OpenCode' })
    expect(screen.queryByRole('button', { name: 'DeepSeek Harness' })).toBeNull()
  })
})
