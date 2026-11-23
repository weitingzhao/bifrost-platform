import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { shellSelfHealthQuery } from '@/domains/foundation/shell/useShellStatusLine'
const authFake = vi.hoisted(() => ({canAdmin: true, capsError: false, token: 'admin-test'}))
vi.mock('@/hooks/usePlatformAuth', () => ({usePlatformAuth: () => authFake}))

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
  authFake.canAdmin = true
  window.localStorage.clear()
})

describe('Ops Harness preview', () => {
  it('hides the launcher from a Guest without fetching the preview config', () => {
    authFake.canAdmin = false
    const fetchConfig = renderPreview('prod', Response.json({ opencode: 'https://opencode-preview.ops.bifrost.lan/' }))
    expect(screen.queryByRole('region', {name: 'Local harnesses'})).toBeNull()
    expect(fetchConfig).not.toHaveBeenCalled()
  })

  it('keeps the Ops bearer on Ops and redeems only the scoped grant at the native origin', async () => {
    window.localStorage.setItem('platform_operator_token', authFake.token)
    const opencode = 'https://opencode-preview.ops.bifrost.lan/project/session'
    const requests = renderPreview('prod', Response.json({ opencode, authorization: 'ops-session' }))
    requests
      .mockResolvedValueOnce(Response.json({ grant: 'one.time' }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
    fireEvent.click(await screen.findByRole('button', { name: 'OpenCode' }))
    await waitFor(() => expect(screen.getByTitle('OpenCode')).toHaveAttribute('src', opencode))
    expect(requests).toHaveBeenNthCalledWith(2, '/api/v1/harness-preview/grants', expect.objectContaining({ headers: expect.objectContaining({Authorization:'Bearer admin-test'}) }))
    expect(requests).toHaveBeenNthCalledWith(3, 'https://opencode-preview.ops.bifrost.lan/.bifrost/session', expect.objectContaining({ body: new URLSearchParams({grant:'one.time'}), credentials:'include' }))
    expect(requests.mock.calls[2][1]).not.toHaveProperty('headers')
  })

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
