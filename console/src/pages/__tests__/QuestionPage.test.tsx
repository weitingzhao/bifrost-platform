import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { APPROVAL_TOKEN_STORAGE_KEY } from '@/api/approvals'
import { QuestionPage } from '@/pages/QuestionPage'

const CONTRACT = join(dirname(fileURLToPath(import.meta.url)), '../../../../api/internal/questions/testdata/contract')
const ID = 'q_0123456789abcdef'

function contractBody(name: string): Record<string, unknown> {
  const file = JSON.parse(readFileSync(join(CONTRACT, name), 'utf8')) as { body: Record<string, unknown> }
  return file.body
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function installStorage(): void {
  const mem = new Map<string, string>()
  const store: Storage = {
    get length() {
      return mem.size
    },
    clear() {
      mem.clear()
    },
    getItem(key) {
      return mem.has(key) ? (mem.get(key) ?? null) : null
    },
    key(index) {
      return [...mem.keys()][index] ?? null
    },
    removeItem(key) {
      mem.delete(key)
    },
    setItem(key, value) {
      mem.set(key, String(value))
    },
  }
  Object.defineProperty(window, 'localStorage', { configurable: true, value: store })
}

function wrapper(children: ReactNode, client = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

function posts(): number {
  return vi.mocked(fetch).mock.calls.filter(call => {
    const url = String(call[0])
    return url.includes('/answer') && call[1]?.method === 'POST'
  }).length
}

describe('QuestionPage', () => {
  beforeEach(() => {
    installStorage()
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, 'admin-token')
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    window.localStorage.removeItem(APPROVAL_TOKEN_STORAGE_KEY)
    window.location.hash = ''
  })

  it('opens #questions?id=q_… and does not request a malformed id', async () => {
    window.location.hash = `#questions?id=${ID}`
    vi.mocked(fetch).mockResolvedValue(json(contractBody('question-open.json')))
    render(wrapper(<QuestionPage />))
    expect(await screen.findByText('Where is the quota?')).toBeTruthy()
    expect(screen.getByText('From an agent thread. Answering approves nothing.')).toBeTruthy()
    expect(screen.getByText('If you do not answer')).toBeTruthy()
    expect(screen.getByText('Meanwhile the thread will')).toBeTruthy()
    expect(screen.getByText('Visible to every agent thread. Do not paste secrets.')).toBeTruthy()
    expect(screen.getByText('Recommended by the agent')).toBeTruthy()
    const called = vi.mocked(fetch).mock.calls.map(call => String(call[0]))
    expect(called.some(url => url.includes(`/api/v1/questions/${ID}`))).toBe(true)

    window.location.hash = '#questions?id=Q12'
    vi.mocked(fetch).mockClear()
    render(wrapper(<QuestionPage />))
    expect(await screen.findByText('This link does not name a question.')).toBeTruthy()
    expect(vi.mocked(fetch).mock.calls.map(call => String(call[0])).some(url => url.includes('/questions'))).toBe(false)
  })

  it('sends nothing when an option is chosen, and Confirm answer sends the contract body', async () => {
    window.location.hash = `#questions?id=${ID}`
    const open = contractBody('question-open.json')
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/answer')) return json(contractBody('answer-response.json'))
      return json(open)
    })
    render(wrapper(<QuestionPage />))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    expect(posts()).toBe(0)
    expect(screen.getByText('Answering approves nothing. A release or a change needs a request.')).toBeTruthy()
    const request = JSON.parse(readFileSync(join(CONTRACT, 'answer-request.json'), 'utf8')) as unknown
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    await waitFor(() => expect(posts()).toBe(1))
    const call = vi.mocked(fetch).mock.calls.find(item => String(item[0]).endsWith('/answer'))
    expect(JSON.parse(String(call?.[1]?.body))).toEqual(request)
  })

  it('disables Confirm answer while the send is in flight', async () => {
    window.location.hash = `#questions?id=${ID}`
    let release: (value: Response) => void = () => undefined
    vi.mocked(fetch).mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/answer')) return new Promise(resolve => { release = resolve })
      return Promise.resolve(json(contractBody('question-open.json')))
    })
    render(wrapper(<QuestionPage />))
    fireEvent.click(await screen.findByRole('button', { name: /a Ship/ }))
    const confirm = screen.getByRole('button', { name: 'Confirm answer' })
    fireEvent.click(confirm)
    await waitFor(() => expect(confirm.hasAttribute('disabled')).toBe(true))
    release(json(contractBody('answer-response.json')))
  })

  it('shows the server answer on 409 and does not keep the form', async () => {
    window.location.hash = `#questions?id=${ID}`
    const conflict = JSON.parse(readFileSync(join(CONTRACT, 'error-already-answered.json'), 'utf8')) as {
      status: number
      body: unknown
    }
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      if (String(input).endsWith('/answer')) return json(conflict.body, conflict.status)
      return json(contractBody('question-open.json'))
    })
    render(wrapper(<QuestionPage />))
    fireEvent.click(await screen.findByRole('button', { name: /a Ship/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    const stored = await screen.findByText('b')
    expect(stored.closest('[data-stored-answer]')).toBeTruthy()
    expect(screen.getByText('The thread has not reported reading it')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Confirm answer' })).toBeNull()
    expect(posts()).toBe(1)
  })

  it('re-reads after a lost response: answered shows the stored answer and sends nothing more', async () => {
    window.location.hash = `#questions?id=${ID}`
    let lost = true
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/answer')) {
        lost = false
        throw new TypeError('Failed to fetch')
      }
      return json(lost ? contractBody('question-open.json') : contractBody('question-answered.json'))
    })
    render(wrapper(<QuestionPage />))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(await screen.findByText('The thread has not reported reading it')).toBeTruthy()
    expect(posts()).toBe(1)
    expect(screen.queryByRole('button', { name: 'Confirm answer' })).toBeNull()
  })

  it('re-reads after a lost response and allows another send when the question is still open', async () => {
    window.location.hash = `#questions?id=${ID}`
    let attempts = 0
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/answer')) {
        attempts += 1
        if (attempts === 1) throw new TypeError('Failed to fetch')
        return json(contractBody('answer-response.json'))
      }
      return json(contractBody('question-open.json'))
    })
    render(wrapper(<QuestionPage />))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(await screen.findByText('The answer was not confirmed. You can send it again.')).toBeTruthy()
    const confirm = screen.getByRole('button', { name: 'Confirm answer' })
    expect(confirm.hasAttribute('disabled')).toBe(false)
    fireEvent.click(confirm)
    await waitFor(() => expect(attempts).toBe(2))
  })

  it('shows a script and a URL in the title and body as text', async () => {
    window.location.hash = `#questions?id=${ID}`
    const open = contractBody('question-open.json')
    open.title = '<script>alert(1)</script>'
    open.body = 'See https://evil.example/phish\nsecond line'
    vi.mocked(fetch).mockResolvedValue(json(open))
    const { container } = render(wrapper(<QuestionPage />))
    expect(await screen.findByText('<script>alert(1)</script>')).toBeTruthy()
    expect(container.querySelector('script')).toBeNull()
    expect(container.querySelector('a[href="https://evil.example/phish"]')).toBeNull()
    expect(screen.getByText(/https:\/\/evil\.example\/phish/)).toBeTruthy()
    const body = screen.getByText(/second line/)
    expect(body.className).toContain('whitespace-pre-wrap')
  })

  it('keeps 360×780 in one column with options at least 44px and the text box at 16px', async () => {
    window.location.hash = `#questions?id=${ID}`
    vi.mocked(fetch).mockResolvedValue(json(contractBody('question-open.json')))
    render(wrapper(<QuestionPage />))
    const page = await screen.findByTestId('question-page')
    page.style.width = '360px'
    page.style.height = '780px'
    expect(page.className).toContain('overflow-x-clip')
    expect(page.className).toContain('flex-col')
    const option = await screen.findByRole('button', { name: /a Ship/ })
    expect(Number.parseFloat(option.style.minHeight)).toBeGreaterThanOrEqual(44)
    const confirmRow = screen.queryByRole('button', { name: 'None of these' })
    expect(Number.parseFloat(confirmRow?.style.minHeight ?? '0')).toBeGreaterThanOrEqual(44)
    const box = document.querySelector('textarea')
    expect(box?.style.fontSize).toBe('16px')
    for (const el of page.querySelectorAll<HTMLElement>('*')) {
      expect(el.scrollWidth).toBeLessThanOrEqual(360)
    }
  })

  it('does not confirm None of these without text', async () => {
    window.location.hash = `#questions?id=${ID}`
    vi.mocked(fetch).mockResolvedValue(json(contractBody('question-open.json')))
    render(wrapper(<QuestionPage />))
    fireEvent.click(await screen.findByRole('button', { name: 'None of these' }))
    const confirm = screen.getByRole('button', { name: 'Confirm answer' })
    expect(confirm.hasAttribute('disabled')).toBe(true)
    fireEvent.click(confirm)
    expect(posts()).toBe(0)
    fireEvent.change(document.querySelector('textarea') as HTMLTextAreaElement, { target: { value: '   ' } })
    expect(confirm.hasAttribute('disabled')).toBe(true)
    fireEvent.change(document.querySelector('textarea') as HTMLTextAreaElement, { target: { value: 'not those' } })
    expect(confirm.hasAttribute('disabled')).toBe(false)
  })

  it('disables the form when the record is expired', async () => {
    window.location.hash = `#questions?id=${ID}`
    vi.mocked(fetch).mockResolvedValue(json(contractBody('question-expired.json')))
    render(wrapper(<QuestionPage />))
    const option = await screen.findByRole('button', { name: /a Ship/ })
    expect(option.hasAttribute('disabled')).toBe(true)
    expect(screen.getByTestId('question-page').querySelector('[data-question-form="disabled"]')).toBeTruthy()
    expect(screen.getByText('Q12 expired with no answer')).toBeTruthy()
    expect(posts()).toBe(0)
  })

  it('says the thread reported reading it at the time the record carries', async () => {
    window.location.hash = `#questions?id=${ID}`
    const answered = contractBody('question-answered.json')
    const answer = answered.answer as Record<string, unknown>
    answer.read = { at: '2026-10-11T08:00:00Z', session: 'thread-a', principal: 'reporter' }
    vi.mocked(fetch).mockResolvedValue(json(answered))
    render(wrapper(<QuestionPage />))
    const when = new Date('2026-10-11T08:00:00Z').toLocaleString()
    expect(await screen.findByText(`The thread reported reading it at ${when}`)).toBeTruthy()
  })

  it('renders a refused link as text', async () => {
    window.location.hash = `#questions?id=${ID}`
    const open = contractBody('question-open.json')
    open.link = 'https://ops.bifrost.lan/#approvals?id=ap-1'
    open.link_text = 'Docs'
    vi.mocked(fetch).mockResolvedValue(json(open))
    const { container } = render(wrapper(<QuestionPage />))
    expect(await screen.findByText('Docs')).toBeTruthy()
    expect(container.querySelector('a[href*="approvals"]')).toBeNull()
  })

  it('shows Opening while the question is loading', async () => {
    window.location.hash = `#questions?id=${ID}`
    vi.mocked(fetch).mockImplementation(() => new Promise(() => undefined))
    render(wrapper(<QuestionPage />))
    expect(await screen.findByText(`Opening ${ID}…`)).toBeTruthy()
    expect(screen.getByTestId('question-page').getAttribute('data-question-state')).toBe('loading')
  })

  it('shows the error words when the question cannot be read', async () => {
    window.location.hash = `#questions?id=${ID}`
    vi.mocked(fetch).mockResolvedValue(json({ error: 'boom' }, 500))
    render(wrapper(<QuestionPage />))
    expect(await screen.findByText('boom')).toBeTruthy()
    expect(screen.getByTestId('question-page').getAttribute('data-question-state')).toBe('unreadable')
    expect(screen.queryByText('Where is the quota?')).toBeNull()
  })

  it('keeps the last record and names a failed refresh', async () => {
    window.location.hash = `#questions?id=${ID}`
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    let fail = false
    vi.mocked(fetch).mockImplementation(async () =>
      fail ? json({ error: 'boom' }, 500) : json(contractBody('question-open.json')),
    )
    render(wrapper(<QuestionPage />, client))
    expect(await screen.findByText('Where is the quota?')).toBeTruthy()
    fail = true
    await client.refetchQueries({ queryKey: ['questions', 'item', ID] })
    expect(await screen.findByText('Could not refresh this question: boom')).toBeTruthy()
    expect(screen.getByText('Where is the quota?')).toBeTruthy()
    expect(screen.getByTestId('question-page').getAttribute('data-question-state')).toBe('stale')
  })
})
