import { QueryClient, QueryClientProvider, onlineManager } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { APPROVAL_TOKEN_STORAGE_KEY } from '@/api/approvals'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { ApprovalsPage } from '@/pages/ApprovalsPage'

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status })
const row = (status = 'pending', id = 'record-a') => buildPlatformApproval({ id, number: id === 'record-a' ? 57 : 58, status })
const waiting = 'Still waiting for the server. Keep this page open.'
const retry = 'The request is still pending. That attempt did not take effect. Try again.'
let client: QueryClient
let read: (id: string) => Promise<Response>
let post: (init: RequestInit) => Promise<Response>
let reads: string[]
let moduleScript: HTMLScriptElement | undefined

beforeEach(() => {
  const values = new Map<string, string>([[APPROVAL_TOKEN_STORAGE_KEY, 'fixture-only']])
  Object.defineProperty(window, 'localStorage', { configurable: true, value: {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  } })
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  reads = []
  read = async id => json(row('pending', id === '58' || id === 'record-b' ? 'record-b' : 'record-a'))
  post = async () => json({ status: 'executed' })
  vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    if (init?.method === 'POST') return post(init)
    if (url === '/index.html') return Promise.resolve(new Response('<script type="module" src="/same.js"></script>'))
    if (url.includes('?status=')) return Promise.resolve(json({ approvals: [] }))
    const id = url.split('/').at(-1)!
    reads.push(id)
    return read(id)
  }))
})
afterEach(() => {
  cleanup(); client.clear(); onlineManager.setOnline(true)
  moduleScript?.remove(); moduleScript = undefined
  vi.useRealTimers(); vi.unstubAllGlobals()
})
async function open(id: string) {
  window.location.hash = `#approvals?id=${id}`
  render(<QueryClientProvider client={client}><ApprovalsPage /></QueryClientProvider>)
  return screen.findByRole('button', { name: /Approve/ })
}
async function tick(ms = 30_000) { await act(async () => { await vi.advanceTimersByTimeAsync(ms) }) }
function clock() { vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval'] }) }
async function navigate(id: string) {
  await act(async () => { window.location.hash = `#approvals?id=${id}`; window.dispatchEvent(new Event('hashchange')) })
  await tick(1)
}

describe.each(['record-a', '57'])('record opened as %s', id => {
  it('does not abort a 60 second POST, warns at 30 seconds, then displays its decision', async () => {
    const button = await open(id)
    clock()
    let finish!: (r: Response) => void
    let signal: AbortSignal | null | undefined
    let aborted = false
    post = init => { signal = init.signal; signal?.addEventListener('abort', () => { aborted = true }); return new Promise(resolve => { finish = resolve }) }
    fireEvent.click(button)
    await tick()
    expect(screen.getByText(waiting)).toBeTruthy()
    await tick()
    expect(aborted).toBe(false)
    expect(signal).toBeUndefined()
    expect(reads).toEqual([id])
    expect(screen.getByRole('button', { name: 'Sending…' }).hasAttribute('disabled')).toBe(true)
    await act(async () => { finish(json({ status: 'executed' })) })
    await tick(1)
    expect(screen.getByText('This has run.')).toBeTruthy()
  })

  it('keeps running decisions polling until failed, then stops (review 1b)', async () => {
    const button = await open(id); clock()
    post = async () => { throw new TypeError('connection lost') }
    read = async () => json(row('running'))
    fireEvent.click(button); await tick(1)
    expect(screen.getByText('Approved. Running now.')).toBeTruthy()
    read = async () => json(row('failed'))
    await tick()
    await tick(1)
    expect(screen.getByText('This failed.')).toBeTruthy()
    const count = reads.length
    await tick(600_000)
    expect(reads).toHaveLength(count)
  })

  it('keeps successful POST cached under the read key when refresh fails (review 2)', async () => {
    const button = await open(id)
    read = async () => json({ error: 'unavailable' }, 503)
    fireEvent.click(button)
    await screen.findByText('This has run.')
    expect(client.getQueryData(['approvals', 'item', id])).toMatchObject({ status: 'executed' })
    expect(screen.queryByRole('button', { name: /Approve/ })).toBeNull()
  })

  it('shows approved after failed POST reconciliation under the read key (review 2)', async () => {
    const button = await open(id)
    post = async () => { throw new TypeError('connection lost') }
    read = async () => json(row('approved'))
    fireEvent.click(button)
    await screen.findByText('Approved. Running now.')
    expect(client.getQueryData(['approvals', 'item', id])).toMatchObject({ status: 'approved' })
    expect(screen.queryByRole('button', { name: /Approve/ })).toBeNull()
  })

  it('keeps the record lock when changing between its number and id', async () => {
    const button = await open(id); clock()
    post = () => new Promise(() => undefined)
    fireEvent.click(button); await tick()
    await navigate(id === '57' ? 'record-a' : '57')
    expect(screen.getByText(waiting)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Sending…' }).hasAttribute('disabled')).toBe(true)
  })

  it('shows the in-flight decision after switching aliases even when refresh fails', async () => {
    const button = await open(id); clock()
    let finish!: (r: Response) => void
    post = () => new Promise(resolve => { finish = resolve })
    fireEvent.click(button)
    await navigate(id === '57' ? 'record-a' : '57')
    read = async () => json({ error: 'read unavailable' }, 503)
    await act(async () => { finish(json({ status: 'executed' })) })
    await tick(1)
    expect(screen.getByText('This has run.')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Approve/ })).toBeNull()
  })

  it('reconciles the active alias without unlocking its stale pending card', async () => {
    const button = await open(id); clock()
    let fail!: (error: Error) => void
    post = () => new Promise((_resolve, reject) => { fail = reject })
    fireEvent.click(button)
    await navigate(id === '57' ? 'record-a' : '57')
    read = async () => json(row('approved'))
    await act(async () => { fail(new TypeError('lost response')) })
    await tick(1)
    expect(screen.getByText('Approved. Running now.')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Approve/ })).toBeNull()
  })

  it('allows terminal GETs to add execution details without regressing status', async () => {
    const button = await open(id)
    read = async () => json({ ...row('executed'), execution: { exit_code: 0, duration_ms: 2000 } })
    fireEvent.click(button)
    await screen.findByText('It ran and finished.')
    expect(screen.getByText(/Exit code 0 · took 2 s/)).toBeTruthy()
  })

  it('keeps A success isolated while showing an executed B (review 3)', async () => {
    const button = await open(id); clock()
    let finish!: (r: Response) => void
    post = () => new Promise(resolve => { finish = resolve })
    fireEvent.click(button); await tick()
    read = async key => json(row('executed', key === id ? 'record-a' : 'record-b'))
    await navigate(id === '57' ? '58' : 'record-b')
    expect(screen.queryByText(waiting)).toBeNull()
    expect(screen.getByText('This has run.')).toBeTruthy()
    await act(async () => { finish(json({ status: 'failed' })) })
    await tick(1)
    expect(screen.getByText('This has run.')).toBeTruthy()
    await navigate(id)
    expect(screen.getByText('This failed.')).toBeTruthy()
  })

  it('applies the same failure reconciliation and server reasons to reject', async () => {
    await open(id)
    fireEvent.change(screen.getByLabelText('Reject reason'), { target: { value: 'not needed' } })
    post = async () => json({ error: 'reject storage unavailable' }, 500)
    read = async () => json(row('rejected'))
    fireEvent.click(screen.getByRole('button', { name: /Reject gitops/ }))
    await screen.findByText('This was rejected.')
    expect(screen.getByText(/reject storage unavailable/)).toBeTruthy()
    expect(reads).toEqual([id, id])
  })

  it.each([
    ['sending', 'pending'], ['unreadable', 'pending'],
    ['sending', 'executed'], ['unreadable', 'executed'],
  ])('isolates A %s from B %s and preserves A on return (review 3)', async (mode, bStatus) => {
    const button = await open(id); clock()
    post = mode === 'sending' ? () => new Promise(() => undefined) : async () => { throw new TypeError('A error') }
    read = async key => key === id ? json({ error: 'missing' }, 404) : json(row(bStatus, 'record-b'))
    fireEvent.click(button); await tick(30_001)
    await navigate(id === '57' ? '58' : 'record-b')
    expect(screen.queryByText(waiting)).toBeNull()
    expect(screen.queryByText(/Could not read this request/)).toBeNull()
    expect(screen.queryByText('A error')).toBeNull()
    if (bStatus === 'pending') expect(screen.getByRole('button', { name: /Approve/ }).hasAttribute('disabled')).toBe(false)
    else expect(screen.queryByRole('button', { name: /Approve/ })).toBeNull()
    await navigate(id)
    expect(screen.getByRole('button', { name: mode === 'sending' ? 'Sending…' : /Approve/ }).hasAttribute('disabled')).toBe(true)
    expect(screen.getByText(mode === 'sending' ? waiting : /Could not read this request/)).toBeTruthy()
  })

  it('clears pending retry advice after a poll sees approved (review 4)', async () => {
    const button = await open(id); clock()
    post = async () => { throw new TypeError('lost') }
    fireEvent.click(button); await tick(1)
    expect(screen.getByText(retry)).toBeTruthy()
    read = async () => json(row('approved'))
    await tick()
    await tick(1)
    expect(screen.getByText('Approved. Running now.')).toBeTruthy()
    expect(screen.queryByText(retry)).toBeNull()
  })

  it.each([409, 500])('retains the server reason for HTTP %s while restoring pending buttons (review 5)', async status => {
    const button = await open(id)
    post = async () => json({ error: 'store approvals: read-only file system' }, status)
    fireEvent.click(button)
    await screen.findByText(retry)
    expect(screen.getByRole('alert').textContent).toContain('store approvals: read-only file system')
    expect(screen.getByRole('button', { name: /Approve/ }).hasAttribute('disabled')).toBe(false)
    expect(reads).toEqual([id, id])
  })

  it('reports unreadable confirmation with time and refresh advice, retries without unlocking (review 6)', async () => {
    const button = await open(id); clock()
    post = async () => { throw new TypeError('network down') }
    read = async () => json({ error: 'missing' }, 404)
    fireEvent.click(button); await tick(1)
    expect(screen.getByText(/Could not read this request\. Last attempt: .+Refresh this page/)).toBeTruthy()
    expect(screen.getByText('network down')).toBeTruthy()
    expect(screen.getByRole('button', { name: /Approve/ }).hasAttribute('disabled')).toBe(true)
    await tick()
    expect(reads).toEqual([id, id, id])
    expect(screen.getByRole('button', { name: /Approve/ }).hasAttribute('disabled')).toBe(true)
  })

  it.each(['pending', 'running'])('cannot regress executed to a late %s GET (review 7a)', async status => {
    const button = await open(id)
    let finish!: (r: Response) => void
    read = () => new Promise(resolve => { finish = resolve })
    fireEvent.click(button)
    await screen.findByText('This has run.')
    await act(async () => { finish(json(row(status))) })
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)) })
    expect(screen.getByText('This has run.')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Approve/ })).toBeNull()
  })

  it('cannot regress approved to pending in a GET started after the click (review 7a)', async () => {
    const button = await open(id)
    let finish!: (r: Response) => void
    read = () => new Promise(resolve => { finish = resolve })
    post = async () => json({ status: 'approved' })
    fireEvent.click(button)
    await screen.findByText('Approved. Running now.')
    await act(async () => { finish(json(row())) })
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)) })
    expect(screen.queryByRole('button', { name: /Approve/ })).toBeNull()
  })

  it('shows the detail offline notice (review 7b)', async () => {
    await open(id)
    onlineManager.setOnline(false)
    void client.refetchQueries({ queryKey: ['approvals', 'item', id] })
    await screen.findByText('Could not read approvals — offline.')
  })

  it.each(['non-2xx', 'unparseable'])('does not invent build drift from %s version checks (review 7c)', async mode => {
    moduleScript = document.createElement('script'); moduleScript.type = 'module'; moduleScript.src = '/same.js'; document.head.append(moduleScript)
    const fetcher = vi.mocked(fetch).getMockImplementation()!
    vi.mocked(fetch).mockImplementation((input, init) => String(input) === '/index.html'
      ? Promise.resolve(mode === 'non-2xx' ? new Response('<script type="module" src="/new.js"></script>', { status: 503 }) : new Response('bad html'))
      : fetcher(input, init))
    const button = await open(id)
    await act(async () => { await Promise.resolve() })
    expect(button.hasAttribute('disabled')).toBe(false)
    expect(screen.queryByText(/A newer Console/)).toBeNull()
  })
})
