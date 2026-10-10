import { readFileSync } from 'node:fs'
import path from 'node:path'
import { describe, expect, it } from 'vitest'
import {
  buildApprovalListResponse,
  buildPlatformApproval,
} from '@/api/approvalsApiFixture'
import {
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
