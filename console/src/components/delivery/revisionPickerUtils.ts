import { validateGitRevision } from '@/lib/delivery/revisionValidation'

export function isRevisionDeployReady(rev: string): boolean {
  return validateGitRevision(rev) == null
}
