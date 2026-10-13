import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { useConsoleBuildDrift } from '@/pwa/useConsoleBuildDrift'
function Probe() { return <p>{useConsoleBuildDrift() ? 'stale' : 'current'}</p> }
const html = (src: string) => new Response(`<script type="module" src="${src}"></script>`)
beforeEach(() => {
  const script = document.createElement('script')
  script.type = 'module'
  script.src = '/assets/build-original.js'
  script.dataset.testBuild = ''
  document.head.append(script)
  vi.stubGlobal('fetch', vi.fn(async () => html('/assets/build-original.js')))
})
afterEach(() => {
  cleanup()
  document.querySelectorAll('script[data-test-build]').forEach(node => node.remove())
  vi.useRealTimers()
  vi.unstubAllGlobals()
})
it('uses no-store with the assertion outside the caught fetch mock', async () => {
  render(<Probe />)
  await act(async () => {})
  expect(vi.mocked(fetch).mock.calls[0][1]?.cache).toBe('no-store')
})
it('checks the build again every three minutes', async () => {
  vi.useFakeTimers()
  render(<Probe />)
  await act(async () => {})
  vi.mocked(fetch).mockResolvedValue(html('/assets/build-next.js'))
  await act(async () => { await vi.advanceTimersByTimeAsync(180_000) })
  expect(screen.getByText('stale')).toBeTruthy()
  expect(fetch).toHaveBeenCalledTimes(2)
})
it('does not overlap checks and an older response cannot clear drift', async () => {
  vi.useFakeTimers()
  let finish!: (value: Response) => void
  vi.mocked(fetch).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  render(<Probe />)
  document.dispatchEvent(new Event('visibilitychange'))
  expect(fetch).toHaveBeenCalledTimes(1)
  await act(async () => { await vi.advanceTimersByTimeAsync(15_000) })
  vi.mocked(fetch).mockResolvedValue(html('/assets/build-next.js'))
  await act(async () => { document.dispatchEvent(new Event('visibilitychange')) })
  expect(screen.getByText('stale')).toBeTruthy()
  await act(async () => { finish(html('/assets/build-original.js')) })
  expect(screen.getByText('stale')).toBeTruthy()
})
it('preserves confirmed drift across page remount and matching responses', async () => {
  vi.mocked(fetch).mockResolvedValue(html('/assets/build-next.js'))
  const view = render(<Probe />)
  await act(async () => {})
  expect(screen.getByText('stale')).toBeTruthy()
  view.unmount()
  vi.mocked(fetch).mockResolvedValue(html('/assets/build-original.js'))
  render(<Probe />)
  await act(async () => {})
  expect(screen.getByText('stale')).toBeTruthy()
})
it('bounds a hanging check and allows a subsequent check without a false warning', async () => {
  vi.useFakeTimers()
  vi.mocked(fetch).mockImplementationOnce(() => new Promise(() => undefined))
  render(<Probe />)
  await act(async () => { await vi.advanceTimersByTimeAsync(15_000) })
  expect(vi.mocked(fetch).mock.calls[0][1]?.signal?.aborted).toBe(true)
  await act(async () => { document.dispatchEvent(new Event('visibilitychange')) })
  expect(fetch).toHaveBeenCalledTimes(2)
  expect(screen.getByText('current')).toBeTruthy()
})
