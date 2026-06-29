/** Validates a git ref before starting a PipelineRun. */
export function validateGitRevision(rev: string): string | null {
  const t = rev.trim()
  if (!t) return 'Revision is required'
  if (t.length > 256) return 'Revision too long (max 256 characters)'
  if (/\s/.test(t)) return 'Revision cannot contain whitespace'
  if (t.startsWith('#') || t.includes('## ')) {
    return 'Invalid revision — enter a branch or tag name, not pasted debug text'
  }
  return null
}
