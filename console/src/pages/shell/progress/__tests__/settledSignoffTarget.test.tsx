import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, render, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { useSettledSignoffTarget } from '../useSettledSignoffTarget'

function Page({ ready }: { ready: boolean }) {
  useSettledSignoffTarget(['M-1'], ready)
  return <><section>Goals loaded above the queue</section><section id="signoff">Waiting for your sign-off</section></>
}
afterEach(() => { cleanup(); vi.restoreAllMocks(); window.history.replaceState(null, '', window.location.pathname) })
it('finishes a cold deep link after asynchronous goal data, then leaves subsequent polls alone', async () => {
  const scroll = vi.fn()
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, value: scroll })
  window.history.replaceState(null, '', '#work?section=signoff')
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  let finish!: (data: unknown) => void
  const reading = client.fetchQuery({ queryKey: ['agent-runtime-goal', 'M-1'], queryFn: () => new Promise(resolve => { finish = resolve }) })
  const tree = (ready: boolean) => <QueryClientProvider client={client}><Page ready={ready} /></QueryClientProvider>
  const view = render(tree(false))
  view.rerender(tree(true)) // Runtime has arrived; the larger goal cards are still loading.
  expect(scroll).not.toHaveBeenCalled()
  await act(async () => { finish({ mission: { title: 'Fixture goal' }, tasks: [] }); await reading })
  await waitFor(() => expect(scroll).toHaveBeenCalledTimes(1))
  expect(scroll.mock.contexts[0]).toBe(document.getElementById('signoff'))
  act(() => { client.setQueryData(['agent-runtime-goal', 'M-1'], { mission: { title: 'Later poll' }, tasks: [] }) })
  await new Promise(resolve => setTimeout(resolve, 80))
  expect(scroll).toHaveBeenCalledTimes(1)
  act(() => { window.dispatchEvent(new HashChangeEvent('hashchange')) })
  await waitFor(() => expect(scroll).toHaveBeenCalledTimes(2))
  client.clear()
})

it('cancels final positioning when the user scrolls before the goal arrives', async () => {
  const scroll = vi.fn()
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, value: scroll })
  window.history.replaceState(null, '', '#work?section=signoff')
  const client = new QueryClient()
  render(<QueryClientProvider client={client}><Page ready /></QueryClientProvider>)
  act(() => { window.dispatchEvent(new WheelEvent('wheel')) })
  act(() => { client.setQueryData(['agent-runtime-goal', 'M-1'], { mission: { title: 'Late fixture' } }) })
  await new Promise(resolve => setTimeout(resolve, 80))
  expect(scroll).not.toHaveBeenCalled()
  act(() => { window.dispatchEvent(new HashChangeEvent('hashchange')) })
  await waitFor(() => expect(scroll).toHaveBeenCalledTimes(1))
  client.clear()
})

it('preserves a goal deep link when the same address also names sign-off', async () => {
  const scroll = vi.fn()
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, value: scroll })
  window.history.replaceState(null, '', '#work?goal=M-1&section=signoff')
  const client = new QueryClient()
  client.setQueryData(['agent-runtime-goal', 'M-1'], { mission: { title: 'Fixture' } })
  render(<QueryClientProvider client={client}><Page ready /></QueryClientProvider>)
  await new Promise(resolve => setTimeout(resolve, 80))
  expect(scroll).not.toHaveBeenCalled()
  client.clear()
})
