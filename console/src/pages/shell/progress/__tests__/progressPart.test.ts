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

  it('truncates on code points so an emoji is not split into a lone surrogate', () => {
    const emoji = '😀'
    const raw = `${'a'.repeat(198)}${emoji}z`
    expect(raw.length).toBe(201)
    expect(Array.from(raw)).toHaveLength(200)
    const kept = boundDocumentText(raw, 200)
    expect(kept).toBe(raw)
    expect(kept).not.toMatch(/\uD83D(?![\uDC00-\uDFFF])/)
    const cut = boundDocumentText(`${raw}q`, 200)
    expect(cut.endsWith('…')).toBe(true)
    expect(cut).toContain(emoji)
    expect(cut).not.toMatch(/\uD83D(?![\uDC00-\uDFFF])/)
    expect(Array.from(cut)).toHaveLength(200)
  })
})
