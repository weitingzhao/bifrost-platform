import { DenseTag } from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import { fetchAgentBridge } from '@/api/agentOps'
import type { GitDirtyRepoDetail } from '@/api/agentTypes'

function fileList(detail: GitDirtyRepoDetail): string[] {
  const out: string[] = []
  for (const f of detail.staged ?? []) out.push(f)
  for (const f of detail.modified ?? []) {
    if (!out.includes(f)) out.push(f)
  }
  for (const f of detail.untracked ?? []) {
    if (!out.includes(f)) out.push(`?${f}`)
  }
  return out
}

export function GitDirtyDetailsPanel({
  className,
  onProposeCommit,
  proposeDisabled,
}: {
  className?: string
  onProposeCommit?: () => void
  proposeDisabled?: boolean
}) {
  const bridgeQ = useQuery({
    queryKey: ['agent', 'bridge'],
    queryFn: fetchAgentBridge,
    refetchInterval: 20_000,
  })
  const gb = bridgeQ.data?.git_bridge
  const dirtyCount = gb?.dirty_repos ?? 0
  const details = gb?.dirty_repo_details ?? []

  if (gb == null || gb.status !== 'ok' || dirtyCount <= 0) {
    return null
  }

  return (
    <div
      className={
        className ??
        'rounded-md border border-amber-500/35 bg-amber-500/5 px-2.5 py-2'
      }
      aria-label="Dirty git repos"
    >
      <div className="flex flex-wrap items-center gap-1.5">
        <span className="text-[var(--text-dense-caption)] font-semibold text-foreground">
          Review dirty repos
        </span>
        <DenseTag variant="warning" className="text-[8px]">
          {dirtyCount} dirty
        </DenseTag>
        <span className="text-[var(--text-dense-micro)] text-muted-foreground">
          Propose commit requires operator approval — not auto-clean
        </span>
      </div>
      {bridgeQ.isLoading ? (
        <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-muted-foreground">
          Loading dirty details…
        </p>
      ) : details.length === 0 ? (
        <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-muted-foreground">
          Bridge reports {dirtyCount} dirty repo(s); file details unavailable — restart platform-api
          after git-bridge upgrade, or open Agent Desk.
        </p>
      ) : (
        <ul className="m-0 mt-1.5 list-none space-y-1 p-0">
          {details.map(d => {
            const files = fileList(d)
            const preview = files.slice(0, 8).join(', ')
            const more = files.length > 8 ? ` (+${files.length - 8})` : ''
            return (
              <li
                key={d.repo}
                className="rounded border border-border/40 bg-background/60 px-2 py-1"
              >
                <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                  <span className="font-mono text-[var(--text-dense-meta)] font-medium text-foreground">
                    {d.repo}
                  </span>
                  {d.branch != null && d.branch !== '' && (
                    <span className="font-mono text-[var(--text-dense-micro)] text-muted-foreground">
                      {d.branch}
                    </span>
                  )}
                  <span className="font-mono text-[var(--text-dense-caption)] text-emerald-700 dark:text-emerald-400">
                    +{d.insertions}
                  </span>
                  <span className="font-mono text-[var(--text-dense-caption)] text-red-700 dark:text-red-400">
                    −{d.deletions}
                  </span>
                </div>
                {preview !== '' && (
                  <p className="m-0 mt-0.5 truncate font-mono text-[var(--text-dense-micro)] text-muted-foreground">
                    {preview}
                    {more}
                  </p>
                )}
              </li>
            )
          })}
        </ul>
      )}
      {onProposeCommit != null && (
        <div className="mt-1.5 flex flex-wrap gap-2">
          <button
            type="button"
            className="text-[var(--text-dense-caption)] font-medium text-primary hover:underline disabled:opacity-50"
            disabled={proposeDisabled}
            onClick={onProposeCommit}
          >
            Propose commit →
          </button>
        </div>
      )}
    </div>
  )
}
