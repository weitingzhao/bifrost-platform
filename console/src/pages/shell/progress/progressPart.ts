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
  const code = char.charCodeAt(0)
  if (code <= 31 || (code >= 127 && code <= 159)) return true
  if (code >= 0x202a && code <= 0x202e) return true
  return code >= 0x2066 && code <= 0x2069
}

/**
 * What an agent says this request belongs to. One line, at most 120 characters.
 * It is not a fact about the request.
 */
export function partOfLine(part: ProgressPart): string {
  let clean = ''
  for (const char of `Agent-reported: ${part.work} — ${part.step}`) {
    if (!isHiddenChar(char)) clean += char
  }
  clean = clean.replace(/\s+/g, ' ').trim()
  if (clean.length <= 120) return clean
  return `${clean.slice(0, 119)}…`
}
