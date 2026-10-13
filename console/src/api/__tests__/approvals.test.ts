import { readFileSync } from 'node:fs'
import path from 'node:path'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  buildApprovalListResponse,
  buildPlatformApproval,
} from '@/api/approvalsApiFixture'
import {
  fetchApprovalList,
  formatDecidedAt,
  isUnsetDecidedAt,
  parseApprovalList,
} from '@/api/approvals'
import { approvalJsonTagsFromGo } from '@/api/approvalGoStructParse'
import { GO_ZERO_DECIDED_AT } from '@/api/approvalsServerContract'

const repoRoot = path.resolve(import.meta.dirname, '../../../..')
const typesGo = readFileSync(path.join(repoRoot, 'api/internal/approvals/types.go'), 'utf8')
const handlerGo = readFileSync(path.join(repoRoot, 'api/internal/approvals/handler.go'), 'utf8')

describe('approvals API contract', () => {
  it('matches Go list envelope (approvals, not items)', () => {
    expect(handlerGo).toMatch(
      /writeJSON\(w, http\.StatusOK, map\[string\]any\{"approvals": out\}\)/,
    )
    expect(handlerGo).not.toMatch(/"items":\s*(?:list|out)\b/)
  })

  it('fixture includes every Go Approval json field', () => {
    const sample = buildPlatformApproval({ id: 'ap-contract' })
    const keys = new Set(Object.keys(sample))
    for (const tag of approvalJsonTagsFromGo(typesGo)) {
      expect(keys.has(tag), `missing fixture field ${tag}`).toBe(true)
    }
  })

  it('parseApprovalList reads server fixture envelope', () => {
    const row = buildPlatformApproval({ id: 'ap-1' })
    const parsed = parseApprovalList(buildApprovalListResponse([row]))
    expect(parsed).toHaveLength(1)
    expect(parsed[0]?.id).toBe('ap-1')
    expect(parsed[0]?.params_hash).toBe(row.params_hash)
  })

  it('parseApprovalList rejects legacy items envelope', () => {
    const row = buildPlatformApproval({ id: 'ap-legacy' })
    expect(() => parseApprovalList({ items: [row] })).toThrow(/unexpected list shape/)
  })
})

describe('approval read deadline', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('aborts a hanging approvals read after 15 seconds', async () => {
    const mem = new Map<string, string>()
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        get length() {
          return mem.size
        },
        clear: () => mem.clear(),
        getItem: (key: string) => mem.get(key) ?? null,
        key: (index: number) => [...mem.keys()][index] ?? null,
        removeItem: (key: string) => mem.delete(key),
        setItem: (key: string, value: string) => mem.set(key, String(value)),
      },
    })
    vi.useFakeTimers()
    vi.stubGlobal(
      'fetch',
      vi.fn((_url: unknown, init?: RequestInit) => {
        return new Promise((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () => {
            reject(new DOMException('The operation was aborted', 'AbortError'))
          })
        })
      }),
    )
    const pending = fetchApprovalList('open').then(
      () => {
        throw new Error('resolved')
      },
      (err: unknown) => err,
    )
    await vi.advanceTimersByTimeAsync(14_999)
    await vi.advanceTimersByTimeAsync(1)
    const err = await pending
    expect(err).toBeInstanceOf(Error)
    expect((err as Error).message).toBe('approvals read timed out')
  })
})

describe('formatDecidedAt', () => {
  it('treats Go zero time as pending', () => {
    expect(isUnsetDecidedAt(GO_ZERO_DECIDED_AT)).toBe(true)
    expect(
      formatDecidedAt({ status: 'pending', decided_at: GO_ZERO_DECIDED_AT }),
    ).toBe('Pending')
    expect(formatDecidedAt({ status: 'pending', decided_at: GO_ZERO_DECIDED_AT })).not.toMatch(
      /0001/,
    )
  })
})
