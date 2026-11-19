// 2026-11-19 23:27-23:50Z, PROD: a tablet came back from a network change with
// its live stream hung on a dead connection (no error, no end). It never
// reconnected and polled at its own pace until the page was reloaded. A stream
// silent for LIVE_IDLE_MS (three missed 15 s heartbeats) is now reopened.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import { QueryClient } from '@tanstack/react-query'

const calls: AbortSignal[] = []
let respond: (signal: AbortSignal) => Promise<Response>

vi.mock('@/api/serverContact', () => ({
  serverFetch: (_input: unknown, init: RequestInit) => {
    calls.push(init.signal as AbortSignal)
    return respond(init.signal as AbortSignal)
  },
}))

const { IdleWatch, LIVE_IDLE_MS, useLiveRefresh } = await import('../liveRefresh')

/** A request that never answers, like one sent on a dead connection; it fails only when aborted. */
function hang(signal: AbortSignal): Promise<Response> {
  return new Promise((_, reject) => signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true }))
}

/** An open stream that writes a heartbeat every 15 s until aborted. */
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

describe('live stream watchdog', () => {
  beforeEach(() => {
    calls.length = 0
    vi.useFakeTimers()
  })
  afterEach(() => vi.useRealTimers())

  it('fires once after ms without a poke, never while poked', () => {
    let fired = 0
    const w = new IdleWatch(1_000, () => fired++)
    for (let i = 0; i < 5; i++) { vi.advanceTimersByTime(900); w.poke() }
    expect(fired).toBe(0)
    vi.advanceTimersByTime(1_000)
    expect(fired).toBe(1)
    w.stop()
  })

  it('drops a hung stream and opens a new one', async () => {
    respond = hang
    const { unmount } = renderHook(() => useLiveRefresh(new QueryClient()))
    await vi.advanceTimersByTimeAsync(LIVE_IDLE_MS - 1_000)
    expect(calls).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1_000 + 2_000) // idle, then the 2 s retry
    expect(calls[0].aborted).toBe(true)
    expect(calls).toHaveLength(2)
    unmount()
    expect(calls[1].aborted).toBe(true)
  })

  it('keeps a stream that beats (positive control for the case above)', async () => {
    respond = beating
    const { unmount } = renderHook(() => useLiveRefresh(new QueryClient()))
    await vi.advanceTimersByTimeAsync(5 * LIVE_IDLE_MS)
    expect(calls).toHaveLength(1)
    expect(calls[0].aborted).toBe(false)
    unmount()
    expect(calls[0].aborted).toBe(true)
  })
})
