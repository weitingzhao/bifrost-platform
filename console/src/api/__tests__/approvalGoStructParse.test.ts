import { describe, expect, it } from 'vitest'
import { approvalJsonTagsFromGo } from '@/api/approvalGoStructParse'

describe('approvalJsonTagsFromGo', () => {
  it('throws when Go source has no Approval struct (parser empty-pass guard)', () => {
    const fakeGo = [
      'package approvals',
      '',
      'type OtherStruct struct {',
      '\tID string `json:"id"`',
      '}',
    ].join('\n')
    expect(() => approvalJsonTagsFromGo(fakeGo)).toThrow(/Go Approval struct not parsed/)
  })
})
