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

export function partOfLine(part: ProgressPart): string {
  return `Part of: ${part.work} — ${part.step}`
}
