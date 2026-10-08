import { Fragment, useMemo, useState } from 'react'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableDetailRow,
  DenseTableHead,
  DenseTableHeader,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTag,
  denseTableNumCell,
} from '@bifrost/ui'
import type { BranchHealth } from '@/api/lineage'

/** Waiting longer than this is worth a look; longer than STALE_DAYS is stale. */
const WARN_DAYS = 3
const STALE_DAYS = 7

type Filter = 'all' | 'open' | 'stale' | 'landed' | 'even'

const FILTERS: { id: Filter; label: string; hint: string }[] = [
  { id: 'all', label: 'All', hint: 'Every non-default branch on GitHub, unlanded work first' },
  { id: 'open', label: 'Unlanded work', hint: 'Branches with changes that are not on main in any form' },
  { id: 'stale', label: 'Stale', hint: 'Head older than the 60-day read and not on main inside it — not measured' },
  { id: 'landed', label: 'Leftovers', hint: 'Every change already landed on main another way (rebased, re-versioned, squashed) — safe to clean up' },
  { id: 'even', label: 'Merged', hint: 'Nothing ahead of main — the branch name is all that is left' },
]

const STATUS_TAG: Record<BranchHealth['status'], { variant: 'warning' | 'danger' | 'info' | 'neutral'; label: string }> = {
  open: { variant: 'warning', label: 'unlanded' },
  stale: { variant: 'danger', label: 'stale' },
  landed: { variant: 'info', label: 'leftover' },
  even: { variant: 'neutral', label: 'merged' },
}

function shortRepo(repo: string): string {
  return repo.replace(/^bifrost-/, '')
}

function days(iso?: string): number | null {
  if (iso == null) return null
  const ms = Date.parse(iso)
  return Number.isFinite(ms) ? (Date.now() - ms) / 86_400_000 : null
}

function ageLabel(d: number | null): string {
  if (d == null) return '—'
  if (d < 1) return `${Math.max(1, Math.round(d * 24))} h`
  return `${d.toFixed(d < 10 ? 1 : 0)} d`
}

function ageClass(d: number | null): string {
  if (d == null) return 'text-muted-foreground'
  if (d >= STALE_DAYS) return 'text-danger font-semibold'
  if (d >= WARN_DAYS) return 'text-warning'
  return ''
}

function threadName(t: { session: string; title?: string }): string {
  if (t.title) return t.title
  if (t.session.startsWith('local_')) return t.session.slice(0, 14)
  const cloud = /session_[A-Za-z0-9]+/.exec(t.session)
  return cloud != null ? `cloud ${cloud[0].slice(0, 16)}` : t.session.slice(0, 20)
}

export function BranchesPanel({ branches, showHelp }: { branches: BranchHealth[]; showHelp: boolean }) {
  const [filter, setFilter] = useState<Filter>('all')
  const [open, setOpen] = useState<Set<string>>(new Set())
  const counts = useMemo(() => {
    const c: Record<Filter, number> = { all: branches.length, open: 0, stale: 0, landed: 0, even: 0 }
    for (const b of branches) c[b.status]++
    return c
  }, [branches])
  const rows = useMemo(() => branches.filter(b => filter === 'all' || b.status === filter), [branches, filter])

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-1">
        {FILTERS.filter(f => f.id !== 'stale' || counts.stale > 0).map(f => (
          <button
            key={f.id}
            type="button"
            title={f.hint}
            onClick={() => setFilter(f.id)}
            className={`inline-flex h-6 items-center gap-1 rounded-[var(--control-radius)] px-2 text-[var(--text-dense-caption)] ${
              filter === f.id ? 'bg-primary text-primary-foreground' : 'bg-[var(--control-fill)] hover:bg-[var(--control-fill-hover)]'
            }`}
          >
            {f.label} <span className="font-mono-tabular opacity-70">{counts[f.id]}</span>
          </button>
        ))}
      </div>
      {showHelp && (
        <p className="text-[var(--text-dense-meta)] text-muted-foreground">
          Every branch except each repo&apos;s main, as the Gitea mirror of GitHub has it — a branch that was never pushed is
          not here, and archiving a session does not hide its branch. Read 60 days back. Ahead = commits main does not have; Open = those whose
          change is not on main in any form (Change-Id, else subject) — real unlanded work. Behind = main commits since the
          branch forked (&quot;+&quot; = forked before what was read). Waiting = age of the oldest open change: yellow after{' '}
          {WARN_DAYS} days, red after {STALE_DAYS}. Branches made before the commit hooks have no Change-Id, so a lane re-landed
          under a new version number can still show as open — check its commits. Stale = the head is older than 60 days and
          not on main within them: abandoned work or an old merge, not measured.
        </p>
      )}
      {rows.length === 0 ? (
        <p className="py-4 text-center text-[var(--text-dense-meta)] text-muted-foreground">No branch in this group.</p>
      ) : (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Repo</DenseTableHead>
              <DenseTableHead>Branch</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
              <DenseTableHead className={denseTableNumCell} title="Changes not on main in any form / commits main does not have">
                Open / ahead
              </DenseTableHead>
              <DenseTableHead className={denseTableNumCell} title="Main commits since the branch forked">
                Behind
              </DenseTableHead>
              <DenseTableHead className={denseTableNumCell} title="Age of the oldest open change">
                Waiting
              </DenseTableHead>
              <DenseTableHead className={denseTableNumCell}>Last activity</DenseTableHead>
              <DenseTableHead>Threads</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {rows.map(b => {
              const id = `${b.repo}/${b.branch}`
              const isOpen = open.has(id)
              const stale = b.status === 'stale'
              // stale: any open change is at least as old as the head
              const wait = stale ? days(b.head_at) : days(b.oldest_open_at)
              return (
                <Fragment key={id}>
                  <DenseTableRow
                    className="cursor-pointer"
                    aria-expanded={isOpen}
                    onClick={() =>
                      setOpen(prev => {
                        const next = new Set(prev)
                        if (next.has(id)) next.delete(id)
                        else next.add(id)
                        return next
                      })
                    }
                  >
                    <DenseTableCell className="whitespace-nowrap">
                      <span className="mr-1 text-muted-foreground">{isOpen ? '▾' : '▸'}</span>
                      {shortRepo(b.repo)}
                    </DenseTableCell>
                    <DenseTableCell className="max-w-[280px] truncate font-mono-tabular" title={b.branch}>
                      {b.branch}
                    </DenseTableCell>
                    <DenseTableCell>
                      <DenseTag variant={STATUS_TAG[b.status].variant}>{STATUS_TAG[b.status].label}</DenseTag>
                    </DenseTableCell>
                    <DenseTableCell className={denseTableNumCell}>
                      {stale ? <span className="text-muted-foreground">—</span> : `${b.open} / ${b.ahead}`}
                    </DenseTableCell>
                    <DenseTableCell className={denseTableNumCell}>
                      {b.behind}
                      {b.behind_is_floor ? '+' : ''}
                    </DenseTableCell>
                    <DenseTableCell className={`${denseTableNumCell} ${ageClass(wait)}`}>
                      {stale ? '≥ ' : ''}
                      {ageLabel(wait)}
                    </DenseTableCell>
                    <DenseTableCell className={`${denseTableNumCell} text-muted-foreground`}>{ageLabel(days(b.head_at))} ago</DenseTableCell>
                    <DenseTableCell className="max-w-[320px] truncate" title={b.threads.map(t => `${threadName(t)} (${t.session})`).join('\n')}>
                      {b.threads.length === 0 ? <span className="text-muted-foreground">—</span> : b.threads.map(threadName).join(' · ')}
                    </DenseTableCell>
                  </DenseTableRow>
                  {isOpen && (
                    <DenseTableDetailRow>
                      <DenseTableCell colSpan={8}>
                        <div className="flex flex-col gap-0.5 py-1">
                          {b.commits.length === 0 && (
                            <span className="text-[var(--text-dense-meta)] text-muted-foreground">
                              {stale
                                ? `Head ${b.head_sha.slice(0, 9)} is older than the 60-day read and not on main within it — not measured. Check the branch on GitHub.`
                                : `Nothing ahead of main: head ${b.head_sha.slice(0, 9)} is on main.`}
                            </span>
                          )}
                          {b.commits.map(c => (
                            <div key={c.sha} className="flex items-center gap-2 text-[var(--text-dense-meta)]">
                              <span className="font-mono-tabular text-muted-foreground">{c.sha.slice(0, 7)}</span>
                              <span className="font-mono-tabular text-muted-foreground">{new Date(c.at).toLocaleString()}</span>
                              <span className="min-w-0 flex-1 truncate">{c.subject}</span>
                              {c.landed_by ? (
                                <DenseTag variant="neutral">
                                  landed via {c.landed_by === 'change_id' ? 'Change-Id' : 'subject'}
                                  {c.landed_sha ? ` · ${c.landed_sha.slice(0, 7)}` : ''}
                                </DenseTag>
                              ) : (
                                <DenseTag variant="warning">not on main</DenseTag>
                              )}
                            </div>
                          ))}
                        </div>
                      </DenseTableCell>
                    </DenseTableDetailRow>
                  )}
                </Fragment>
              )
            })}
          </DenseTableBody>
        </DenseDataTable>
      )}
    </div>
  )
}
