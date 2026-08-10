import { cn } from '@bifrost/ui'
import { CheckCircle2, XCircle } from 'lucide-react'
import type { RefPreflightResponse } from '@/api/deliveryTypes'
import { validateGitRevision } from '@/lib/delivery/revisionValidation'

const KIND_LABEL: Record<string, string> = {
  branch: 'branch',
  tag: 'tag',
  commit: 'commit',
  missing: 'missing',
  error: 'unverified',
}

export function RefPreflightStatus({
  data,
  isLoading,
  revision,
}: {
  data: RefPreflightResponse | undefined
  isLoading: boolean
  revision: string
}) {
  const trimmed = revision.trim()
  if (!trimmed || validateGitRevision(trimmed) != null) return null

  if (isLoading) {
    return (
      <span className="text-dense-caption text-muted-foreground/60">
        Checking ref across repos…
      </span>
    )
  }
  if (data == null) return null

  const repoClass = (kind: string, exists: boolean): string => {
    if (exists) return 'text-muted-foreground/70'
    if (kind === 'error') return 'text-warning'
    return 'text-destructive font-medium'
  }

  return (
    <div className="flex flex-col gap-1">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        {data.repos.map(repo => (
          <span
            key={repo.repo}
            className={cn('inline-flex items-center gap-1 text-dense-caption font-mono', repoClass(repo.kind, repo.exists))}
            title={repo.detail || `${repo.repo}: ${repo.kind}`}
          >
            {repo.exists
              ? <CheckCircle2 className="h-3 w-3 text-success/60" />
              : <XCircle className="h-3 w-3" />}
            {repo.repo}
            <span className="text-muted-foreground/40">{KIND_LABEL[repo.kind] ?? repo.kind}</span>
          </span>
        ))}
      </div>
      {data.missing.length > 0 && (
        <span className="text-dense-caption text-destructive">
          ⚠ Revision missing in: {data.missing.join(', ')} — create the same branch/tag there before deploying.
        </span>
      )}
      {data.missing.length === 0 && data.reachability !== 'ok' && (
        <span className="text-dense-caption text-warning">
          Could not verify all repos — deploy allowed, but clone may still fail if a ref is absent.
        </span>
      )}
    </div>
  )
}
