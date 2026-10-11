import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { APPROVAL_TOKEN_STORAGE_KEY } from '@/api/approvals'
import { parseQuestion } from '@/api/questions'
import { QuestionPage } from '@/pages/QuestionPage'
import { resetAnswerSendStore } from '@/pages/question/answerSendStore'
import { QuestionRow } from '@/pages/shell/needs-you/QuestionRow'

const CONTRACT = join(dirname(fileURLToPath(import.meta.url)), '../../../../api/internal/questions/testdata/contract')
const ID = 'q_0123456789abcdef'
const ID_B = 'q_0000000000000002'

function freshClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
}

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
    resetAnswerSendStore()
    installStorage()
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, 'admin-token')
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    cleanup()
    resetAnswerSendStore()
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

  it('stays disabled when a lost response cannot be re-read, and Check again only re-reads', async () => {
    window.location.hash = `#questions?id=${ID}`
    let gets = 0
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/answer')) throw new TypeError('Failed to fetch')
      gets += 1
      if (gets === 1) return json(contractBody('question-open.json'))
      throw new TypeError('Failed to fetch')
    })
    render(wrapper(<QuestionPage />))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(await screen.findByText('Could not confirm whether the answer was saved.')).toBeTruthy()
    const confirm = screen.getByRole('button', { name: 'Confirm answer' })
    expect(confirm.hasAttribute('disabled')).toBe(true)
    expect(posts()).toBe(1)
    const getsAfterFailure = gets
    fireEvent.click(screen.getByRole('button', { name: 'Check again' }))
    await waitFor(() => expect(gets).toBe(getsAfterFailure + 1))
    expect(posts()).toBe(1)
    expect(confirm.hasAttribute('disabled')).toBe(true)
  })

  it('shows the posted answer when the follow-up read fails', async () => {
    window.location.hash = `#questions?id=${ID}`
    let gets = 0
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/answer')) return json(contractBody('answer-response.json'))
      gets += 1
      if (gets === 1) return json(contractBody('question-open.json'))
      throw new TypeError('Failed to fetch')
    })
    render(wrapper(<QuestionPage />))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    const stored = await screen.findByText('The thread has not reported reading it')
    expect(stored.closest('[data-stored-answer]')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Confirm answer' })).toBeNull()
    expect(posts()).toBe(1)
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
    expect(screen.queryByRole('button', { name: 'Confirm answer' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /b Wait/ }))
    const confirm = screen.getByRole('button', { name: 'Confirm answer' })
    expect(confirm.hasAttribute('disabled')).toBe(false)
    fireEvent.click(confirm)
    await waitFor(() => expect(attempts).toBe(2))
  })

  it('shows a script in every record text as text and adds no element', async () => {
    const mark = (field: string) => `<script>${field}</script>`
    window.location.hash = `#questions?id=${ID}`
    const open = contractBody('question-open.json')
    open.title = mark('title')
    open.body = `${mark('body')}\nsecond line`
    open.urgency = mark('urgency')
    open.meanwhile = mark('meanwhile')
    open.requester_thread = mark('requester_thread')
    open.work_id = mark('work_id')
    open.link = 'https://evil.example/phish'
    open.link_text = mark('link_text')
    const options = open.options as { key: string; label: string; detail?: string }[]
    const ship = options.find(option => option.key === 'a')
    const wait = options.find(option => option.key === 'b')
    if (ship != null) ship.label = mark('label')
    if (wait != null) wait.detail = mark('detail')
    vi.mocked(fetch).mockResolvedValue(json(open))
    const page = render(wrapper(<QuestionPage />))
    for (const field of ['title', 'body', 'urgency', 'meanwhile', 'link_text', 'label', 'detail']) {
      expect(await screen.findByText(mark(field), { exact: false })).toBeTruthy()
    }
    expect(page.container.querySelector('script')).toBeNull()
    expect(page.container.querySelector('a[href="https://evil.example/phish"]')).toBeNull()
    const body = screen.getByText(/second line/)
    expect(body.className).toContain('whitespace-pre-wrap')
    page.unmount()

    const answered = contractBody('question-answered.json')
    const answer = answered.answer as Record<string, unknown>
    answer.text = mark('answer')
    vi.mocked(fetch).mockResolvedValue(json(answered))
    const answeredPage = render(wrapper(<QuestionPage />))
    expect(await screen.findByText(mark('answer'), { exact: false })).toBeTruthy()
    expect(answeredPage.container.querySelector('script')).toBeNull()
    answeredPage.unmount()

    const parsed = parseQuestion(open)
    expect(parsed.ok).toBe(true)
    if (!parsed.ok) return
    const row = render(
      <ul>
        <QuestionRow question={parsed.question} />
      </ul>,
    )
    for (const field of ['title', 'work_id', 'requester_thread']) {
      expect(row.container.textContent).toContain(mark(field))
    }
    expect(row.container.querySelector('script')).toBeNull()
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
    fireEvent.click(option)
    const confirm = screen.getByRole('button', { name: 'Confirm answer' })
    expect(Number.parseFloat(confirm.style.minHeight)).toBeGreaterThanOrEqual(44)
    const confirmRow = document.querySelector('[data-confirm-row]')
    expect(confirmRow).toBeTruthy()
    expect(Number.parseFloat((confirmRow as HTMLElement).style.minHeight)).toBeGreaterThanOrEqual(44)
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

  it('drops A’s choice when the hash changes to B, and the request is B’s', async () => {
    const client = freshClient()
    const a = contractBody('question-open.json')
    a.allow_text = true
    const b = contractBody('question-open.json')
    b.id = ID_B
    b.number = 2
    b.title = 'The other question'
    b.allow_text = true
    const parsedA = parseQuestion(a)
    const parsedB = parseQuestion(b)
    expect(parsedA.ok && parsedB.ok).toBe(true)
    if (!parsedA.ok || !parsedB.ok) return
    client.setQueryData(['questions', 'item', ID], parsedA.question)
    client.setQueryData(['questions', 'item', ID_B], parsedB.question)
    window.location.hash = `#questions?id=${ID}`
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/answer')) return json(contractBody('answer-response.json'))
      if (url.includes(ID_B)) return json(b)
      return json(a)
    })
    render(wrapper(<QuestionPage />, client))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.change(document.querySelector('textarea') as HTMLTextAreaElement, { target: { value: 'from A' } })
    expect(document.querySelector('[data-confirm-row]')?.textContent).toContain('from A')
    const callsBefore = vi.mocked(fetch).mock.calls.length
    window.location.hash = `#questions?id=${ID_B}`
    expect(await screen.findByText('The other question')).toBeTruthy()
    expect(vi.mocked(fetch).mock.calls.length).toBe(callsBefore)
    expect(posts()).toBe(0)
    expect(document.querySelector('[data-confirm-row]')).toBeNull()
    expect((document.querySelector('textarea') as HTMLTextAreaElement).value).toBe('')
    expect(screen.queryByText('Where is the quota?')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /a Ship/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    await waitFor(() => expect(posts()).toBe(1))
    const call = vi.mocked(fetch).mock.calls.find(item => String(item[0]).includes('/answer'))
    expect(String(call?.[0])).toContain(`/api/v1/questions/${ID_B}/answer`)
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({ channel: 'console', option: 'a' })
  })

  it('withdraws the confirm row when the chosen option’s label changes', async () => {
    window.location.hash = `#questions?id=${ID}`
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    let label = 'Wait'
    vi.mocked(fetch).mockImplementation(async () => {
      const open = contractBody('question-open.json')
      const options = open.options as { key: string; label: string; detail?: string }[]
      const choice = options.find(option => option.key === 'b')
      if (choice != null) choice.label = label
      return json(open)
    })
    render(wrapper(<QuestionPage />, client))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    const row = document.querySelector('[data-confirm-row]')
    expect(row?.textContent).toContain('b')
    expect(row?.textContent).toContain('Wait')
    expect(row?.textContent).toContain('one day')
    label = 'Later'
    await client.refetchQueries({ queryKey: ['questions', 'item', ID] })
    expect(await screen.findByText('The question changed. Choose again.')).toBeTruthy()
    expect(document.querySelector('[data-confirm-row]')).toBeNull()
    expect(posts()).toBe(0)
  })

  it('does not show A’s answer on B', async () => {
    const client = freshClient()
    const a = contractBody('question-open.json')
    const b = contractBody('question-open.json')
    b.id = ID_B
    b.number = 2
    b.title = 'The other question'
    const answered = contractBody('question-answered.json')
    const parsedA = parseQuestion(a)
    const parsedB = parseQuestion(b)
    expect(parsedA.ok && parsedB.ok).toBe(true)
    if (!parsedA.ok || !parsedB.ok) return
    client.setQueryData(['questions', 'item', ID], parsedA.question)
    client.setQueryData(['questions', 'item', ID_B], parsedB.question)
    window.location.hash = `#questions?id=${ID}`
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/answer')) return json(contractBody('answer-response.json'))
      if (url.includes(ID_B)) return json(b)
      return json(answered)
    })
    render(wrapper(<QuestionPage />, client))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(await screen.findByText('The thread has not reported reading it')).toBeTruthy()
    window.location.hash = `#questions?id=${ID_B}`
    expect(await screen.findByText('The other question')).toBeTruthy()
    expect(screen.queryByText('Where is the quota?')).toBeNull()
    expect(screen.queryByText('The thread has not reported reading it')).toBeNull()
    expect(document.querySelector('[data-stored-answer]')).toBeNull()
  })

  it('does not show a 409 on A after the hash changes to B', async () => {
    const client = freshClient()
    const a = contractBody('question-open.json')
    const b = contractBody('question-open.json')
    b.id = ID_B
    b.number = 2
    b.title = 'The other question'
    const conflict = JSON.parse(readFileSync(join(CONTRACT, 'error-already-answered.json'), 'utf8')) as {
      status: number
      body: unknown
    }
    const parsedA = parseQuestion(a)
    const parsedB = parseQuestion(b)
    expect(parsedA.ok && parsedB.ok).toBe(true)
    if (!parsedA.ok || !parsedB.ok) return
    client.setQueryData(['questions', 'item', ID], parsedA.question)
    client.setQueryData(['questions', 'item', ID_B], parsedB.question)
    window.location.hash = `#questions?id=${ID}`
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      if (String(input).endsWith('/answer')) return json(conflict.body, conflict.status)
      if (String(input).includes(ID_B)) return json(b)
      return json(a)
    })
    render(wrapper(<QuestionPage />, client))
    fireEvent.click(await screen.findByRole('button', { name: /a Ship/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    const stored = await screen.findByText('The thread has not reported reading it')
    expect(stored.closest('[data-stored-answer]')).toBeTruthy()
    window.location.hash = `#questions?id=${ID_B}`
    expect(await screen.findByText('The other question')).toBeTruthy()
    expect(screen.queryByText('The thread has not reported reading it')).toBeNull()
    expect(document.querySelector('[data-stored-answer]')).toBeNull()
  })

  it('shows a later read report on the question that was answered', async () => {
    const client = freshClient()
    const open = contractBody('question-open.json')
    const answered = contractBody('question-answered.json')
    const withRead = contractBody('question-answered.json')
    const answer = withRead.answer as Record<string, unknown>
    answer.read = { at: '2026-10-11T08:00:00Z', session: 'thread-a', principal: 'reporter' }
    const parsed = parseQuestion(open)
    expect(parsed.ok).toBe(true)
    if (!parsed.ok) return
    client.setQueryData(['questions', 'item', ID], parsed.question)
    window.location.hash = `#questions?id=${ID}`
    let reads = 0
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      if (String(input).endsWith('/answer')) return json(contractBody('answer-response.json'))
      reads += 1
      return json(reads === 1 ? answered : withRead)
    })
    render(wrapper(<QuestionPage />, client))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(await screen.findByText('The thread has not reported reading it')).toBeTruthy()
    vi.mocked(fetch).mockImplementation(async () => json(withRead))
    await client.refetchQueries({ queryKey: ['questions', 'item', ID] })
    const when = new Date('2026-10-11T08:00:00Z').toLocaleString()
    expect(await screen.findByText(`The thread reported reading it at ${when}`)).toBeTruthy()
  })

  it('keeps the two fixed lines outside the state branches', async () => {
    const fromAgent = 'From an agent thread. Answering approves nothing.'
    const secrets = 'Visible to every agent thread. Do not paste secrets.'
    const cases: { name: string; hash: string; body: () => Promise<Response> | Response; secrets: boolean }[] = [
      { name: 'loading', hash: `#questions?id=${ID}`, body: () => new Promise(() => undefined), secrets: false },
      { name: 'unreadable', hash: `#questions?id=${ID}`, body: () => json({ error: 'boom' }, 500), secrets: false },
      { name: 'malformed', hash: '#questions?id=Q12', body: () => json({}), secrets: false },
      { name: 'open', hash: `#questions?id=${ID}`, body: () => json(contractBody('question-open.json')), secrets: true },
      { name: 'answered', hash: `#questions?id=${ID}`, body: () => json(contractBody('question-answered.json')), secrets: true },
      { name: 'withdrawn', hash: `#questions?id=${ID}`, body: () => json(contractBody('question-withdrawn.json')), secrets: true },
      { name: 'expired', hash: `#questions?id=${ID}`, body: () => json(contractBody('question-expired.json')), secrets: true },
    ]
    for (const item of cases) {
      window.location.hash = item.hash
      vi.mocked(fetch).mockImplementation(item.body)
      const view = render(wrapper(<QuestionPage />))
      expect(await screen.findByText(fromAgent), item.name).toBeTruthy()
      if (item.secrets) expect(await screen.findByText(secrets), item.name).toBeTruthy()
      else expect(screen.queryByText(secrets), item.name).toBeNull()
      view.unmount()
    }
  })

  it('treats a truncated 200 as unknown and does not enable Confirm answer', async () => {
    window.location.hash = `#questions?id=${ID}`
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      if (String(input).endsWith('/answer')) {
        return new Response('{"id":"q_0123456789abcdef","status":"answered","answer":', {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      return json(contractBody('question-open.json'))
    })
    render(wrapper(<QuestionPage />))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(await screen.findByText('The answer was not confirmed. You can send it again.')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Confirm answer' })).toBeNull()
    expect(posts()).toBe(1)
    fireEvent.click(screen.getByRole('button', { name: /b Wait/ }))
    expect(screen.getByRole('button', { name: 'Confirm answer' }).hasAttribute('disabled')).toBe(false)
  })

  it('does not restore Confirm answer when the read after a saved post returns open', async () => {
    window.location.hash = `#questions?id=${ID}`
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      if (String(input).endsWith('/answer')) return json(contractBody('answer-response.json'))
      return json(contractBody('question-open.json'))
    })
    render(wrapper(<QuestionPage />))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    const stored = await screen.findByText('The thread has not reported reading it')
    expect(stored.closest('[data-stored-answer]')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Confirm answer' })).toBeNull()
    expect(posts()).toBe(1)
  })

  it('keeps a saved answer when an in-flight read later returns open', async () => {
    const client = freshClient()
    const open = contractBody('question-open.json')
    const parsed = parseQuestion(open)
    expect(parsed.ok).toBe(true)
    if (!parsed.ok) return
    client.setQueryData(['questions', 'item', ID], parsed.question)
    let releaseLate: (value: Response) => void = () => undefined
    let sawLate = false
    vi.mocked(fetch).mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/answer')) return Promise.resolve(json(contractBody('answer-response.json')))
      if (!sawLate) {
        sawLate = true
        return new Promise(resolve => {
          releaseLate = resolve
        })
      }
      return Promise.resolve(json(contractBody('question-answered.json')))
    })
    window.location.hash = `#questions?id=${ID}`
    render(wrapper(<QuestionPage />, client))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    const refresh = client.refetchQueries({ queryKey: ['questions', 'item', ID] })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    const stored = await screen.findByText('The thread has not reported reading it')
    expect(stored.closest('[data-stored-answer]')).toBeTruthy()
    await act(async () => {
      releaseLate(json(open))
      await refresh.catch(() => undefined)
    })
    const cached = client.getQueryData<{ status?: string }>(['questions', 'item', ID])
    expect(cached?.status, 'stale open read overwrote the saved answer').toBe('answered')
    expect(screen.getByText('The thread has not reported reading it')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Confirm answer' })).toBeNull()
    expect(posts()).toBe(1)
  })

  it('withdraws the old confirm when a lost response is re-read open with a new label', async () => {
    window.location.hash = `#questions?id=${ID}`
    let posted = false
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      if (String(input).endsWith('/answer')) {
        posted = true
        throw new TypeError('Failed to fetch')
      }
      const open = contractBody('question-open.json')
      if (posted) {
        const options = open.options as { key: string; label: string }[]
        const choice = options.find(option => option.key === 'b')
        if (choice != null) choice.label = 'Later'
      }
      return json(open)
    })
    render(wrapper(<QuestionPage />))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(await screen.findByText('Could not confirm whether the answer was saved.')).toBeTruthy()
    expect(await screen.findByText(/Later/)).toBeTruthy()
    const confirm = screen.queryByRole('button', { name: 'Confirm answer' })
    if (confirm != null) expect(confirm.hasAttribute('disabled')).toBe(true)
    expect(screen.getByRole('button', { name: 'Check again' })).toBeTruthy()
    expect(posts()).toBe(1)
  })

  it('keeps Check again when a refresh changes the option during unknown', async () => {
    window.location.hash = `#questions?id=${ID}`
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    let gets = 0
    let label = 'Wait'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/answer')) throw new TypeError('Failed to fetch')
      gets += 1
      if (gets === 1) return json(contractBody('question-open.json'))
      if (gets === 2) throw new TypeError('Failed to fetch')
      const open = contractBody('question-open.json')
      const options = open.options as { key: string; label: string }[]
      const choice = options.find(option => option.key === 'b')
      if (choice != null) choice.label = label
      return json(open)
    })
    render(wrapper(<QuestionPage />, client))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(await screen.findByText('Could not confirm whether the answer was saved.')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Check again' })).toBeTruthy()
    label = 'Later'
    await client.refetchQueries({ queryKey: ['questions', 'item', ID] })
    expect(await screen.findByText(/Later/)).toBeTruthy()
    expect(screen.getByText('Could not confirm whether the answer was saved.')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Check again' })).toBeTruthy()
    const getsAfterRefresh = gets
    fireEvent.click(screen.getByRole('button', { name: 'Check again' }))
    await waitFor(() => expect(gets).toBeGreaterThan(getsAfterRefresh))
    expect(posts()).toBe(1)
    const confirm = screen.queryByRole('button', { name: 'Confirm answer' })
    if (confirm != null) expect(confirm.hasAttribute('disabled')).toBe(true)
  })
})
