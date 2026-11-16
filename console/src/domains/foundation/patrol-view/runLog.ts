import type { PatrolRun } from '@/api/patrol'

/** Combined stored dispatch log — error then evidence. Patrol V1 has no live token stream. */
export function patrolRunLogText(run: PatrolRun): string {
  const err = run.error?.trim() ?? ''
  const evidence = run.evidence?.trim() ?? ''
  if (err !== '' && evidence !== '') return `${err}\n\n${evidence}`
  if (err !== '') return err
  if (evidence !== '') return evidence
  return ''
}
