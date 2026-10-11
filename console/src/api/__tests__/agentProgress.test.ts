import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  actionLink,
  AGENT_PROGRESS_TIMEOUT_MS,
  expectationLine,
  fetchAgentProgress,
  formatLocalTime,
  ownerNextLine,
  generationIsSkewed,
  GENERATION_SKEW_SECONDS,
  parseAgentProgress,
  parseProgressDocument,
  receivedIsStale,
  STALE_AFTER_SECONDS,
} from '@/api/agentProgress'

const NOW = Date.parse('2026-10-10T15:00:00Z')

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('parseProgressDocument', () => {
  it('drops elements of the wrong kind instead of throwing', () => {
    const doc = parseProgressDocument({
      at: 1,
      verdict: 2,
      owner_now: [null, { name: 'Keep', since: 'yesterday', actions: [null, { text: 'Go', done: 1 }] }],
      works: 'nope',
      needs_you: [null, 'you', 3],
      recent: [1, 'line'],
      feed: [null, { text: 'hi', at: 'x' }],
      owner_next: [],
      coordinator: 'watching',
    })
    expect(doc).toEqual({
      at: 1,
      owner_now: [{ name: 'Keep', actions: [{ text: 'Go' }] }],
      needs_you: ['you'],
      recent: ['line'],
      feed: [{ text: 'hi' }],
    })
    expect(parseProgressDocument(null)).toBeNull()
    expect(parseProgressDocument([1])).toBeNull()
    expect(parseAgentProgress('nope').document).toBeNull()
  })
})

describe('progress freshness', () => {
  it('is stale only after five minutes', () => {
    const received = new Date(NOW - STALE_AFTER_SECONDS * 1000).toISOString()
    expect(receivedIsStale(received, 0, NOW)).toBe(false)
    expect(receivedIsStale(received, 0, NOW + 1000)).toBe(true)
  })

  it('treats a document more than two minutes from publication as skewed', () => {
    const received = new Date(NOW).toISOString()
    expect(generationIsSkewed(NOW / 1000 - GENERATION_SKEW_SECONDS, received, NOW)).toBe(false)
    expect(generationIsSkewed(NOW / 1000 - GENERATION_SKEW_SECONDS - 1, received, NOW)).toBe(true)
    expect(generationIsSkewed(NOW / 1000 + GENERATION_SKEW_SECONDS, received, NOW)).toBe(false)
    expect(generationIsSkewed(NOW / 1000 + GENERATION_SKEW_SECONDS + 1, received, NOW)).toBe(true)
    expect(generationIsSkewed(NOW / 1000 + GENERATION_SKEW_SECONDS + 1, '', NOW)).toBe(true)
  })
})

describe('actionLink', () => {
  it('names the destination of a trusted or hash link and hides the rest', () => {
    expect(actionLink({ text: 'Open Needs you', link: 'https://ops.bifrost.lan/needs', link_text: 'Open Needs you' })).toEqual({
      kind: 'anchor',
      href: 'https://ops.bifrost.lan/needs',
      label: 'Open Needs you (ops.bifrost.lan)',
      external: true,
    })
    expect(actionLink({ text: 'Open', link: 'http://OPS.BIFROST.LAN/x' }).label).toBe('Open (ops.bifrost.lan)')
    expect(actionLink({ text: 'Jump', link: '#progress' })).toEqual({
      kind: 'anchor',
      href: '#progress',
      label: 'Jump (this console)',
      external: false,
    })
    expect(actionLink({ text: 'Read the note', link: 'https://evil.example/phish', link_text: 'Approve the release' })).toEqual({
      kind: 'blocked',
      text: 'Read the note',
      host: 'evil.example',
    })
    expect(actionLink({ text: 'Open', link: 'https://ops.bifrost.lan:443/needs' })).toEqual({
      kind: 'anchor',
      href: 'https://ops.bifrost.lan/needs',
      label: 'Open (ops.bifrost.lan)',
      external: true,
    })
    expect(actionLink({ text: 'Open', link: 'http://ops.bifrost.lan:80/needs' })).toEqual({
      kind: 'anchor',
      href: 'http://ops.bifrost.lan/needs',
      label: 'Open (ops.bifrost.lan)',
      external: true,
    })
    expect(actionLink({ text: 'Open', link: 'https://ops.bifrost.lan:8443/' })).toEqual({
      kind: 'blocked',
      text: 'Open',
      host: 'ops.bifrost.lan:8443',
    })
  })

  it('cleans and bounds the fallback label, and does not build the destination from it', () => {
    const controls = '\u0000\u0007\u061C\u200E\u200F\u202E\uFEFF'
    const link = `#${'a'.repeat(4998)}\u200F`
    expect(link).toHaveLength(5000)
    const bounded = actionLink({ text: controls, link_text: controls, link })
    expect(bounded).toMatchObject({ kind: 'anchor', href: link, external: false })
    if (bounded.kind !== 'anchor') return
    expect(bounded.href).toBe(link)
    expect(bounded.label.includes('\u200F')).toBe(false)
    expect(bounded.label.includes('\u0000')).toBe(false)
    expect(bounded.label.length).toBeLessThanOrEqual(200)
    expect(bounded.label).not.toBe('Open link')
    expect(bounded.href).not.toBe(bounded.label)

    const hidden = `#${'\u200F'.repeat(4999)}`
    expect(hidden).toHaveLength(5000)
    expect(actionLink({ text: controls, link_text: controls, link: hidden })).toEqual({
      kind: 'anchor',
      href: hidden,
      label: 'Open link',
      external: false,
    })

    const trusted = `https://ops.bifrost.lan/${'b'.repeat(5000)}`
    const longUrl = actionLink({ link: trusted })
    expect(longUrl).toMatchObject({ kind: 'anchor', external: true })
    if (longUrl.kind !== 'anchor') return
    expect(longUrl.href).toBe(trusted)
    expect(longUrl.href).not.toBe(longUrl.label)
    expect(longUrl.label.length).toBeLessThanOrEqual(200)
    expect(longUrl.label).not.toBe('Open link')

    expect(actionLink({ link: 'https://ops.bifrost.lan/needs' })).toEqual({
      kind: 'anchor',
      href: 'https://ops.bifrost.lan/needs',
      label: 'https://ops.bifrost.lan/needs (ops.bifrost.lan)',
      external: true,
    })
    expect(actionLink({ text: 'Read the note', link_text: '\u200F', link: 'https://ops.bifrost.lan/needs' }).label).toBe(
      'Read the note (ops.bifrost.lan)',
    )
    expect(actionLink({ text: 'Body', link_text: 'Link name', link: '#progress' })).toEqual({
      kind: 'anchor',
      href: '#progress',
      label: 'Link name (this console)',
      external: false,
    })
    expect(actionLink({ link: '#progress' }).label).toBe('#progress (this console)')
    expect(actionLink({ link: '#' })).toEqual({
      kind: 'anchor',
      href: '#',
      label: 'Open link',
      external: false,
    })
  })
})

describe('formatLocalTime', () => {
  it('renders a timestamp too large to be a date as unknown time', () => {
    expect(formatLocalTime(1e16)).toBe('unknown time')
    expect(formatLocalTime(1e16)).not.toContain('Invalid Date')
    expect(ownerNextLine({ name: 'Ship', at: 1e16 })).toBe('Ship · expected around unknown time')
    expect(expectationLine({ eta_end: 1e16 })).toBe('expected unknown time')
    expect(formatLocalTime(NOW / 1000)).not.toBe('unknown time')
  })
})

describe('fetchAgentProgress', () => {
  it('aborts a hanging read after 10 seconds', async () => {
    const mem = new Map<string, string>()
    const store: Storage = {
      get length() {
        return mem.size
      },
      clear() {
        mem.clear()
      },
      getItem(key) {
        return mem.get(key) ?? null
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
    vi.useFakeTimers()
    let signal: AbortSignal | undefined
    vi.stubGlobal(
      'fetch',
      vi.fn((_url: unknown, init?: RequestInit) => {
        signal = init?.signal
        return new Promise((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () => {
            reject(new DOMException('The operation was aborted', 'AbortError'))
          })
        })
      }),
    )
    const pending = fetchAgentProgress().then(
      () => {
        throw new Error('resolved')
      },
      (err: unknown) => err,
    )
    await vi.advanceTimersByTimeAsync(AGENT_PROGRESS_TIMEOUT_MS - 1)
    expect(signal?.aborted).toBe(false)
    await vi.advanceTimersByTimeAsync(1)
    const err = await pending
    expect(err).toBeInstanceOf(Error)
    expect((err as Error).message).toBe('timed out')
    expect(signal?.aborted).toBe(true)
  })
})
