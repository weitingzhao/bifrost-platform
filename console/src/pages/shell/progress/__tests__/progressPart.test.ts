import { describe, expect, it } from 'vitest'
import { boundDocumentText, partOfLine, sanitizeDocumentText } from '@/pages/shell/progress/progressPart'

const MARKS: Array<[string, string]> = [
  ['U+061C', '\u061C'],
  ['U+200E', '\u200E'],
  ['U+200F', '\u200F'],
  ['U+200B', '\u200B'],
  ['U+200C', '\u200C'],
  ['U+200D', '\u200D'],
  ['U+2060', '\u2060'],
  ['U+FEFF', '\uFEFF'],
]

describe('document text beside a request', () => {
  it.each(MARKS)('strips %s', (_name, mark) => {
    expect(sanitizeDocumentText(`left${mark}right`)).toBe('leftright')
    expect(partOfLine({ work: `work${mark}`, step: `step${mark}` })).toBe('Agent-reported: work — step')
    expect(boundDocumentText(`name${mark}`, 120).includes(mark)).toBe(false)
  })
})
