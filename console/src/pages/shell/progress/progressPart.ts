import type { ProgressDocument } from '@/api/agentProgress'

export type ProgressPart = { work: string; step: string }

/**
 * Where a request sits in the published progress document.
 * Absent document, or an id the document does not mention, returns null.
 */
export function partOfReference(doc: ProgressDocument | null | undefined, id: string): ProgressPart | null {
  if (doc == null || id === '') return null
  for (const entry of doc.owner_now ?? []) {
    if (entry.approval?.id !== id) continue
    const work = entry.work?.trim() ?? ''
    const step = entry.name?.trim() ?? ''
    if (work === '' && step === '') continue
    return { work, step }
  }
  for (const work of doc.works ?? []) {
    const workName = work.short?.trim() || work.title?.trim() || ''
    for (const step of work.steps ?? []) {
      if (step.approval?.id !== id) continue
      const stepName = step.name?.trim() ?? ''
      if (workName === '' && stepName === '') continue
      return { work: workName, step: stepName }
    }
    if (work.owner?.approval?.id === id) {
      const stepName = work.owner.name?.trim() ?? ''
      if (workName !== '' || stepName !== '') return { work: workName, step: stepName }
    }
  }
  return null
}

function isHiddenChar(char: string): boolean {
  const code = char.codePointAt(0) ?? 0
  if (code <= 31 || (code >= 127 && code <= 159)) return true
  if (code === 0x061c || code === 0x200e || code === 0x200f || code === 0x2060 || code === 0xfeff) return true
  if (code >= 0x200b && code <= 0x200d) return true
  if (code >= 0x202a && code <= 0x202e) return true
  return code >= 0x2066 && code <= 0x2069
}

/**
 * Document text shown beside a request. Drops controls, bidi marks, and
 * zero-width characters, then collapses whitespace.
 */
export function sanitizeDocumentText(value: string): string {
  let clean = ''
  for (const char of value) {
    if (!isHiddenChar(char)) clean += char
  }
  return clean.replace(/\s+/g, ' ').trim()
}

/** Same cleaning, then at most `max` characters. A longer value ends with an ellipsis. */
export function boundDocumentText(value: string, max: number): string {
  const clean = sanitizeDocumentText(value)
  if (clean.length <= max) return clean
  return `${clean.slice(0, Math.max(0, max - 1))}…`
}

/**
 * What an agent says this request belongs to. One line, at most 120 characters.
 * It is not a fact about the request.
 */
export function partOfLine(part: ProgressPart): string {
  return boundDocumentText(`Agent-reported: ${part.work} — ${part.step}`, 120)
}
