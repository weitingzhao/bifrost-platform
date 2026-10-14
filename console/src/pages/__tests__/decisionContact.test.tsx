import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { authedFetch } from '@/api/client'
import { getServerContact, recordServerContact, resetServerContact, serverFetch, subscribeServerContact } from '@/api/serverContact'
import { ConnectionCheckError, withDecisionContact } from '@/pages/decisionContact'
import { ServerContactStatus } from '@/pages/ServerContactStatus'
import { APPROVAL_WAITING_NOTICE } from '@/pages/approvalWaitingNotice'
import { NEVER_REACHED, NOT_SENT, notHeardNotice, notSentNotice, sentNotice } from '@/pages/serverContactNotice'

describe('server contact and the decision gate', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 9, 13, 12, 34, 56))
    resetServerContact()
    vi.stubGlobal('fetch', vi.fn())
    // The sign-in token is read from localStorage on every request.
    const values = new Map<string, string>()
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: (key: string) => values.get(key) ?? null,
        setItem: (key: string, value: string) => { values.set(key, value) },
        removeItem: (key: string) => { values.delete(key) },
        clear: () => { values.clear() },
      },
    })
  })
  afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals() })

  it.each([200, 401, 500])('records HTTP %s before authenticated error handling', async status => {
    const changed = vi.fn()
    const unsubscribe = subscribeServerContact(changed)
    vi.mocked(fetch).mockResolvedValue(new Response('{}', { status }))
    await authedFetch('Read', '/api/v1/self-health').catch(() => undefined)
    expect(getServerContact()).toBe(Date.now())
    expect(changed).toHaveBeenCalledOnce()
    unsubscribe()
  })

  it.each([new TypeError('offline'), new DOMException('aborted', 'AbortError')])('does not record failed fetch: %s', async error => {
    vi.mocked(fetch).mockRejectedValue(error)
    await expect(serverFetch('/api/v1/self-health')).rejects.toThrow()
    expect(getServerContact()).toBeNull()
  })

  it('sends immediately within ten seconds without a probe', async () => {
    recordServerContact()
    vi.setSystemTime(Date.now() + 10_000)
    const send = vi.fn().mockResolvedValue('done')
    expect(await withDecisionContact(send, vi.fn())).toBe('done')
    expect(send).toHaveBeenCalledOnce()
    expect(fetch).not.toHaveBeenCalled()
  })

  it.each([200, 401, 500])('probes an old contact, then sends after HTTP %s', async status => {
    recordServerContact()
    vi.setSystemTime(Date.now() + 10_001)
    const order: string[] = []
    vi.mocked(fetch).mockImplementation(async () => { order.push('probe'); return new Response('{}', { status }) })
    await withDecisionContact(async () => { order.push('send') }, vi.fn())
    expect(order).toEqual(['probe', 'send'])
    expect(fetch).toHaveBeenCalledOnce()
    expect(vi.mocked(fetch).mock.calls[0][1]?.cache).toBe('no-store')
  })

  it.each(['timeout', 'network'])('does not send on %s; keeps the last contact and supports a retry', async failure => {
    recordServerContact()
    const last = Date.now()
    vi.setSystemTime(last + 11_000)
    const send = vi.fn().mockResolvedValue('done')
    const notice = vi.fn()
    vi.mocked(fetch).mockImplementation(() => failure === 'network'
      ? Promise.reject(new TypeError('offline')) : new Promise(() => undefined))
    const result = withDecisionContact(send, notice).catch(error => error)
    await vi.advanceTimersByTimeAsync(5000)
    expect(await result).toBeInstanceOf(ConnectionCheckError)
    expect(send).not.toHaveBeenCalled()
    expect(notice).toHaveBeenLastCalledWith(notSentNotice(last))
    expect(getServerContact()).toBe(last)
    vi.mocked(fetch).mockResolvedValue(new Response('{}'))
    await withDecisionContact(send, notice)
    expect(send).toHaveBeenCalledOnce()
  })

  it('says never reached when all reads and the first probe failed', async () => {
    vi.mocked(fetch).mockRejectedValue(new TypeError('offline'))
    const send = vi.fn()
    const notice = vi.fn()
    await expect(withDecisionContact(send, notice)).rejects.toBeInstanceOf(ConnectionCheckError)
    expect(send).not.toHaveBeenCalled()
    expect(notice).toHaveBeenLastCalledWith(notSentNotice(null))
    expect(notSentNotice(null)).toContain(NEVER_REACHED)
    expect(notSentNotice(null)).toContain(NOT_SENT)
  })

  it('shows five and thirty second notices, clears them on reply, and never aborts the POST', async () => {
    recordServerContact()
    const sentAt = Date.now()
    let reply!: (response: Response) => void
    const abort = vi.fn()
    vi.mocked(fetch).mockImplementation((_input, init) => {
      init?.signal?.addEventListener('abort', abort)
      return new Promise(resolve => { reply = resolve })
    })
    const notice = vi.fn()
    const pending = withDecisionContact(() => serverFetch('/api/v1/approvals/a/approve', { method: 'POST' }), notice)
    await vi.advanceTimersByTimeAsync(4999)
    expect(notice).toHaveBeenLastCalledWith(null)
    await vi.advanceTimersByTimeAsync(1)
    expect(notice).toHaveBeenLastCalledWith(sentNotice(sentAt))
    await vi.advanceTimersByTimeAsync(25_000)
    expect(notice).toHaveBeenLastCalledWith(APPROVAL_WAITING_NOTICE)
    expect(abort).not.toHaveBeenCalled()
    expect(vi.mocked(fetch).mock.calls[0][1]?.signal).toBeUndefined()
    reply(new Response('{}'))
    await pending
    expect(notice).toHaveBeenLastCalledWith(null)
    expect(abort).not.toHaveBeenCalled()
  })

  it('does not turn a late aborted probe into contact', async () => {
    let reply!: (response: Response) => void
    vi.mocked(fetch).mockImplementation(() => new Promise(resolve => { reply = resolve }))
    const send = vi.fn()
    const pending = withDecisionContact(send, vi.fn()).catch(error => error)
    await vi.advanceTimersByTimeAsync(5000)
    await pending
    reply(new Response('{}'))
    await Promise.resolve()
    expect(getServerContact()).toBeNull()
    expect(send).not.toHaveBeenCalled()
  })

  it('shows silence beyond sixty seconds and clears on a successful read', async () => {
    recordServerContact()
    const last = Date.now()
    render(<ServerContactStatus />)
    await act(async () => { await vi.advanceTimersByTimeAsync(61_000) })
    expect(screen.getByText(notHeardNotice(last))).toBeTruthy()
    vi.mocked(fetch).mockResolvedValue(new Response('{}'))
    await act(async () => { await serverFetch('/api/v1/self-health') })
    expect(screen.queryByText(notHeardNotice(last))).toBeNull()
  })
})
