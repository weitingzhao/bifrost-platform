// 2026-11-20, PROD: the Bus page's own stream (client=console, receipts=1) had
// no watchdog; the Console's live refresh got one on 11-19 after a tablet's
// stream hung on a dead connection after a network change. The same 45 s rule
// now covers this stream: a hung request is dropped and reopened.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderHook } from '@testing-library/react'

const calls: AbortSignal[] = []
let respond: (signal: AbortSignal) => Promise<Response>

vi.mock('@/api/serverContact', () => ({
  serverFetch: (_input: unknown, init: RequestInit) => {
    calls.push(init.signal as AbortSignal)
    return respond(init.signal as AbortSignal)
  },
}))

const { useBusStream } = await import('../useBusStream')
const { LIVE_IDLE_MS } = await import('../liveRefresh')

function hang(signal: AbortSignal): Promise<Response> {
  return new Promise((_, reject) => signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true }))
}

function beating(signal: AbortSignal): Promise<Response> {
  const enc = new TextEncoder()
  let t: ReturnType<typeof setInterval> | undefined
  const body = new ReadableStream<Uint8Array>({
    start(c) {
      c.enqueue(enc.encode('retry: 2000\n: connected\n\n'))
      t = setInterval(() => c.enqueue(enc.encode(': heartbeat\n\n')), 15_000)
      signal.addEventListener('abort', () => { clearInterval(t); c.error(new DOMException('aborted', 'AbortError')) }, { once: true })
    },
    cancel() { clearInterval(t) },
  })
  return Promise.resolve(new Response(body, { status: 200 }))
}

describe('Bus page stream watchdog', () => {
  beforeEach(() => {
    calls.length = 0
    vi.useFakeTimers()
  })
  afterEach(() => vi.useRealTimers())

  // 2026-11-22: City and collaboration must observe one transport, not open one stream per look.
  it('shares one reader across simultaneous observers and stops only after the last leaves', async () => {
    respond = hang
    const first = renderHook(() => useBusStream())
    const second = renderHook(() => useBusStream())
    expect(calls).toHaveLength(1)
    first.unmount()
    expect(calls[0].aborted).toBe(false)
    second.unmount()
    expect(calls[0].aborted).toBe(true)
  })

  it('drops a hung stream and opens a new one', async () => {
    respond = hang
    const { unmount } = renderHook(() => useBusStream())
    await vi.advanceTimersByTimeAsync(LIVE_IDLE_MS - 1_000)
    expect(calls).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1_000 + 2_000)
    expect(calls[0].aborted).toBe(true)
    expect(calls).toHaveLength(2)
    unmount()
  })

  it('keeps a stream that beats (positive control for the case above)', async () => {
    respond = beating
    const { result, unmount } = renderHook(() => useBusStream())
    await vi.advanceTimersByTimeAsync(5 * LIVE_IDLE_MS)
    expect(calls).toHaveLength(1)
    expect(result.current.connection).toBe('live')
    unmount()
  })
})
