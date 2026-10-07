import { Fragment, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableDetailRow,
  DenseTableHead,
  DenseTableHeader,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTag,
  Input,
  denseTableNumCell,
} from '@bifrost/ui'
import {
  fetchBranches,
  fetchLineage,
  setThreadTitle,
  type LineageCommit,
  type LineageReach,
  type LineageResponse,
  type LineageThread,
} from '@/api/lineage'
import { OpsSection } from '@/components/layout/OpsSection'
import { OpsVerdictStrip, type OpsVerdictLamp, type OpsVerdictTagVariant } from '@/components/layout/OpsVerdictStrip'
import { PageToolbar } from '@/components/layout/PageToolbar'
import { BranchesPanel } from '@/components/lineage/BranchesPanel'
import { LineageGraph } from '@/components/lineage/LineageGraph'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

const WINDOWS = [7, 14, 30, 90] as const
const HELP_KEY = 'lineage.help'

function readHelp(): boolean {
  try {
    return window.localStorage.getItem(HELP_KEY) === '1'
  } catch {
    return false
  }
}

function writeHelp(on: boolean) {
  try {
    window.localStorage.setItem(HELP_KEY, on ? '1' : '0')
  } catch {
    // private window / blocked storage: the toggle still works for this visit
  }
}

const meta = 'text-[var(--text-dense-meta)] text-muted-foreground'

function formatTime(iso: string): string {
  const ms = Date.parse(iso)
  if (!Number.isFinite(ms)) return iso || '—'
  const d = new Date(ms)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** local_9bfc2845-… → local_9bfc2845 · https://claude.ai/code/session_01Du… → cloud session_01Du5yDL */
/** age of a mirror fetch relative to when the response was built */
function mirrorAgeMs(iso: string | undefined, ref: string): number | null {
  if (!iso) return null
  const ms = Date.parse(ref) - Date.parse(iso)
  return Number.isFinite(ms) ? Math.max(0, ms) : null
}

function mirrorAgeLabel(ms: number | null): string {
  if (ms == null) return '—'
  const min = Math.round(ms / 60_000)
  if (min < 1) return 'just now'
  if (min < 90) return `${min} min ago`
  return `${(ms / 3_600_000).toFixed(1)} h ago`
}

function mirrorSummary(data: LineageResponse): string {
  const ages = data.coverage
    .map(c => ({ repo: c.repo, ms: mirrorAgeMs(c.mirror_updated, data.generated_at) }))
    .filter((a): a is { repo: string; ms: number } => a.ms != null)
  if (ages.length === 0) return ' · Gitea mirror'
  const oldest = ages.reduce((a, b) => (b.ms > a.ms ? b : a))
  const pending = data.mirror_sync?.pending?.length ?? 0
  if (oldest.ms < 5 * 60_000) return ' · mirrors fetched from GitHub just now'
  return (
    ` · oldest mirror fetch ${mirrorAgeLabel(oldest.ms)} (${shortRepo(oldest.repo)})` +
    (pending > 0 ? ` · ${pending} still fetching` : '')
  )
}

function threadLabel(session: string): string {
  if (session === '') return 'No session (Cursor / manual)'
  if (session.startsWith('local_')) return session.slice(0, 14)
  const cloud = /session_[A-Za-z0-9]+/.exec(session)
  if (cloud != null) return `cloud ${cloud[0].slice(0, 16)}`
  return session.length > 24 ? `${session.slice(0, 24)}…` : session
}

function shortRepo(repo: string): string {
  return repo.replace(/^bifrost-/, '')
}

function matches(t: LineageThread, needle: string): LineageThread | null {
  if (needle === '') return t
  const n = needle.toLowerCase()
  if (t.session.toLowerCase().includes(n) || t.transcripts.some(x => x.toLowerCase().includes(n))) return t
  const repos = t.repos
    .map(r => ({
      repo: r.repo,
      commits: r.commits.filter(
        c =>
          c.sha.startsWith(n) ||
          (c.change_id ?? '').toLowerCase().startsWith(n) ||
          c.subject.toLowerCase().includes(n) ||
          c.repo.toLowerCase().includes(n),
      ),
    }))
    .filter(r => r.commits.length > 0)
  if (repos.length === 0) return null
  const commits = repos.flatMap(r => r.commits)
  const reached: Record<string, number> = {}
  for (const c of commits) for (const r of c.reached ?? []) reached[`${r.lane}/${r.env}`] = (reached[`${r.lane}/${r.env}`] ?? 0) + 1
  return { ...t, repos, commit_count: commits.length, landed: commits.filter(c => c.landed).length, reached }
}

/** Thread name for display: its title when one is known, else a short id. */
function threadName(t: LineageThread): string {
  return t.title != null && t.title !== '' ? t.title : threadLabel(t.session)
}

/** Inline rename for one thread (operator). Empty saves clear a hand-set name. */
function ThreadTitleEditor({
  thread,
  onDone,
}: {
  thread: LineageThread
  onDone: (changed: boolean) => void
}) {
  const [value, setValue] = useState(thread.title_source === 'manual' ? (thread.title ?? '') : '')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const save = async () => {
    setBusy(true)
    setErr(null)
    try {
      await setThreadTitle(thread.session, value.trim())
      onDone(true)
    } catch (e) {
      setErr((e as Error).message)
      setBusy(false)
    }
  }
  return (
    <span className="inline-flex items-center gap-1" onClick={e => e.stopPropagation()}>
      <Input
        autoFocus
        value={value}
        maxLength={120}
        placeholder={thread.title_source === 'transcript' ? `${thread.title} (synced) — type to override` : 'Name this thread'}
        onChange={e => setValue(e.target.value)}
        onKeyDown={e => {
          if (e.key === 'Enter') void save()
          if (e.key === 'Escape') onDone(false)
        }}
        className="h-6 w-[280px] text-[var(--text-dense-caption)]"
      />
      <Button type="button" size="xs" disabled={busy} onClick={() => void save()}>
        {busy ? 'Saving…' : value.trim() === '' && thread.title_source === 'manual' ? 'Clear' : 'Save'}
      </Button>
      <Button type="button" size="xs" variant="ghost" onClick={() => onDone(false)}>
        Cancel
      </Button>
      {err != null && <span className="text-[var(--text-dense-meta)] text-danger">{err}</span>}
    </span>
  )
}

/** A commit is in production once a deploying release in any lane's "prod" env contained it. */
function inProd(c: LineageCommit): boolean {
  return (c.reached ?? []).some(r => r.deploys && r.env === 'prod')
}

/**
 * Repos some deploying PROD release builds. Commits elsewhere (repos shipped as
 * image builds plus a pin) cannot be "in PROD" by a run, so they are left out of
 * the in-PROD ratio instead of counting as missing.
 */
function prodRepos(data: LineageResponse | undefined): Set<string> {
  const out = new Set<string>()
  for (const r of data?.releases ?? []) if (r.deploys && r.env === 'prod') for (const repo of Object.keys(r.repos)) out.add(repo)
  return out
}

function reachLabel(r: LineageReach): string {
  return r.deploys ? `${r.lane} ${r.env.toUpperCase()}` : `${r.lane} ${r.env}`
}

function ReachedTags({ c }: { c: LineageCommit }) {
  const reached = c.reached ?? []
  if (reached.length === 0) return <span className={meta}>{c.landed ? 'no release yet' : '—'}</span>
  return (
    <span className="flex flex-wrap gap-1">
      {reached.map(r => (
        <span key={`${r.lane}/${r.env}`} title={`First in ${r.run} · ${formatTime(r.at)}${r.deploys ? '' : ' · image build, deployed separately'}`}>
          <DenseTag variant={r.deploys ? (r.env === 'prod' ? 'success' : 'info') : 'neutral'}>{reachLabel(r)}</DenseTag>
        </span>
      ))}
    </span>
  )
}

function LandedTag({ c }: { c: LineageCommit }) {
  if (!c.landed) return <DenseTag variant="warning">not on main</DenseTag>
  if (c.landed_by === 'sha') return <DenseTag variant="success">on main</DenseTag>
  const byChangeId = c.landed_by === 'change_id'
  const why = byChangeId
    ? 'A main commit carries this Change-Id'
    : 'No Change-Id (made before the hooks); a main commit has the same subject'
  return (
    <span title={`${why}${c.landed_sha != null ? ` — main ${c.landed_sha}` : ''}`} className="whitespace-nowrap">
      <DenseTag variant={byChangeId ? 'success' : 'info'}>{byChangeId ? 'via Change-Id' : 'via subject'}</DenseTag>
    </span>
  )
}

function ThreadCommits({ thread }: { thread: LineageThread }) {
  const rows = thread.repos.flatMap(r => r.commits).sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
  return (
    <div className="flex flex-col gap-1 py-1">
      {thread.transcripts.length > 0 && (
        <p className={meta}>
          Transcript{thread.transcripts.length > 1 ? 's' : ''}:{' '}
          <span className="font-mono-tabular">{thread.transcripts.join(' · ')}</span>
        </p>
      )}
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Time</DenseTableHead>
            <DenseTableHead>Repo</DenseTableHead>
            <DenseTableHead>Commit</DenseTableHead>
            <DenseTableHead>Subject</DenseTableHead>
            <DenseTableHead>Ref</DenseTableHead>
            <DenseTableHead>Landed</DenseTableHead>
            <DenseTableHead>Reached</DenseTableHead>
            <DenseTableHead>Change-Id</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {rows.map(c => (
            <DenseTableRow key={`${c.repo}:${c.sha}`}>
              <DenseTableCell className="font-mono-tabular whitespace-nowrap">{formatTime(c.at)}</DenseTableCell>
              <DenseTableCell className="whitespace-nowrap">{shortRepo(c.repo)}</DenseTableCell>
              <DenseTableCell className="font-mono-tabular">{c.sha.slice(0, 9)}</DenseTableCell>
              <DenseTableCell className="w-full max-w-0 truncate" title={c.subject}>
                {c.subject}
              </DenseTableCell>
              <DenseTableCell className="max-w-[160px] truncate font-mono-tabular" title={c.ref}>
                {c.ref}
              </DenseTableCell>
              <DenseTableCell>
                <LandedTag c={c} />
              </DenseTableCell>
              <DenseTableCell>
                <ReachedTags c={c} />
              </DenseTableCell>
              <DenseTableCell className="font-mono-tabular" title={c.change_id}>
                {c.change_id != null ? c.change_id.slice(0, 10) : '—'}
              </DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>
    </div>
  )
}

function verdict(data: LineageResponse | undefined, isError: boolean, isLoading: boolean) {
  if (isError) return { lamp: 'fail' as OpsVerdictLamp, tag: 'danger' as OpsVerdictTagVariant, label: 'API DOWN' }
  if (isLoading || data == null) return { lamp: 'unknown' as OpsVerdictLamp, tag: 'neutral' as OpsVerdictTagVariant, label: 'LOADING' }
  const named = data.threads.filter(t => t.session !== '').length
  const lamp: OpsVerdictLamp = data.reachability === 'ok' ? 'ok' : data.reachability === 'degraded' ? 'degraded' : 'fail'
  const tag: OpsVerdictTagVariant = data.reachability === 'ok' ? 'neutral' : 'warning'
  return { lamp, tag, label: `${named} THREAD${named === 1 ? '' : 'S'}` }
}

export function CommitLineagePage() {
  const [days, setDays] = useState<number>(14)
  const [needle, setNeedle] = useState('')
  const [open, setOpen] = useState<Set<string>>(new Set())
  const qc = useQueryClient()
  const { canOperate } = usePlatformAuth()
  const [renaming, setRenaming] = useState<string | null>(null)
  const [help, setHelp] = useState<boolean>(readHelp)
  const toggleHelp = () =>
    setHelp(h => {
      writeHelp(!h)
      return !h
    })

  const bq = useQuery({
    queryKey: ['lineage-branches'],
    queryFn: () => fetchBranches(),
    staleTime: 5 * 60_000,
    retry: false,
  })
  const waiting = useMemo(() => {
    const open = (bq.data?.branches ?? []).filter(b => b.status === 'open')
    const oldest = open.map(b => Date.parse(b.oldest_open_at ?? '')).filter(Number.isFinite)
    const days = oldest.length > 0 ? (Date.now() - Math.min(...oldest)) / 86_400_000 : null
    return { count: open.length, days }
  }, [bq.data])

  const q = useQuery({
    queryKey: ['lineage', days],
    queryFn: () => fetchLineage(days, false, true),
    staleTime: 60_000,
    retry: false,
  })
  const refresh = async () => {
    const [fresh, br] = await Promise.all([fetchLineage(days, true, true), fetchBranches(true)])
    qc.setQueryData(['lineage', days], fresh)
    qc.setQueryData(['lineage-branches'], br)
  }

  const data = q.data
  const threads = useMemo(
    () => (data?.threads ?? []).map(t => matches(t, needle.trim())).filter((t): t is LineageThread => t != null),
    [data, needle],
  )
  const v = verdict(data, q.isError, q.isLoading)
  const deployedRepos = useMemo(() => prodRepos(data), [data])

  const totals = useMemo(() => {
    const cov = data?.coverage ?? []
    const main = cov.reduce((s, c) => s + c.main_commits, 0)
    const traced = cov.reduce((s, c) => s + c.with_session, 0)
    const untraced = cov.reduce((s, c) => s + c.agent_no_lineage, 0)
    const pending = (data?.threads ?? []).reduce((s, t) => s + (t.commit_count - t.landed), 0)
    const named = (data?.threads ?? []).filter(t => t.session !== '').flatMap(t => t.repos.flatMap(r => r.commits))
    const deployed = prodRepos(data)
    const notInProd = named.filter(c => c.landed && deployed.has(c.repo) && !inProd(c)).length
    return { main, traced, untraced, pending, notInProd }
  }, [data])

  const summary =
    data == null
      ? 'Which agent thread landed which commits — read from commit trailers on the Gitea mirror.'
      : `${totals.traced} of ${totals.main} main commits in the last ${data.days} days name their thread` +
        (totals.pending > 0 ? ` · ${totals.pending} thread commit${totals.pending === 1 ? '' : 's'} not on main yet` : '') +
        (totals.notInProd > 0 ? ` · ${totals.notInProd} landed, not in PROD yet` : '') +
        (waiting.count > 0
          ? ` · ${waiting.count} branch${waiting.count === 1 ? '' : 'es'} with unlanded work${waiting.days != null ? ` (oldest ${waiting.days.toFixed(1)} d)` : ''}`
          : '') +
        mirrorSummary(data)

  const toggle = (id: string) =>
    setOpen(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      <OpsVerdictStrip
        title="COMMIT LINEAGE"
        lamp={v.lamp}
        tagLabel={v.label}
        tagVariant={v.tag}
        summary={summary}
        actions={
          <Button type="button" size="xs" variant="outline" disabled={q.isFetching} onClick={() => void refresh()}>
            {q.isFetching ? 'Loading…' : 'Refresh'}
          </Button>
        }
      />

      <PageToolbar align="between">
        <div className="flex items-center gap-1">
          {WINDOWS.map(d => (
            <Button
              key={d}
              type="button"
              size="xs"
              variant={d === days ? 'default' : 'ghost'}
              onClick={() => setDays(d)}
            >
              {d}d
            </Button>
          ))}
        </div>
        <div className="flex items-center gap-2">
          <Input
            value={needle}
            onChange={e => setNeedle(e.target.value)}
            placeholder="Filter threads: session, Change-Id, sha, subject, repo"
            className="h-6 w-[340px] text-[var(--text-dense-caption)]"
          />
          <Button type="button" size="xs" variant={help ? 'default' : 'ghost'} onClick={toggleHelp} title="Show or hide the explanations on this page">
            {help ? 'Hide how to read' : 'How to read'}
          </Button>
        </div>
      </PageToolbar>

      {data?.graph != null && data.graph.length > 0 && (
        <OpsSection
          title="Graph"
          overflow="visible"
        >
          <LineageGraph graph={data.graph} threads={data.threads} showHelp={help} />
        </OpsSection>
      )}

      <OpsSection title={`Branches${waiting.count > 0 ? ` · ${waiting.count} with unlanded work` : ''}`} overflow="visible">
        {bq.isLoading ? (
          <p className={`py-4 text-center ${meta}`}>Reading every branch…</p>
        ) : bq.error != null ? (
          <p className={`py-4 text-center ${meta}`}>Branches unavailable — {(bq.error as Error).message}</p>
        ) : (
          <>
            <BranchesPanel branches={bq.data?.branches ?? []} showHelp={help} />
            {(bq.data?.errors ?? []).length > 0 && <p className={`pt-2 ${meta}`}>{bq.data?.errors.join(' · ')}</p>}
          </>
        )}
      </OpsSection>

      <OpsSection
        title="Threads"
        description={help ? "A thread is a Claude-Session trailer. Expand a row for its commits; landing is decided by sha, then Change-Id (rebased, version-bumped or squashed), then subject for commits made before the hooks." : undefined}
        overflow="visible"
      >
        {q.isLoading ? (
          <p className={`py-4 text-center ${meta}`}>Loading…</p>
        ) : q.error != null ? (
          <p className={`py-4 text-center ${meta}`}>Lineage API unavailable — {(q.error as Error).message}</p>
        ) : threads.length === 0 ? (
          <p className={`py-4 text-center ${meta}`}>
            {needle !== '' ? 'No thread matches the filter.' : 'No commit in this window carries a lineage trailer.'}
          </p>
        ) : (
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Thread</DenseTableHead>
                <DenseTableHead>Repos</DenseTableHead>
                <DenseTableHead className={denseTableNumCell}>Commits</DenseTableHead>
                <DenseTableHead className={denseTableNumCell}>Landed</DenseTableHead>
                <DenseTableHead className={denseTableNumCell} title="Landed commits in repos a PROD deploy builds, and how many a PROD release already contains">
                  In PROD
                </DenseTableHead>
                <DenseTableHead>Last commit</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {threads.map(t => {
                const id = t.session || '(none)'
                const isOpen = open.has(id)
                const eligible = t.repos.flatMap(r => r.commits).filter(c => c.landed && deployedRepos.has(c.repo))
                const prodCount = eligible.filter(inProd).length
                return (
                  <Fragment key={id}>
                    <DenseTableRow
                      className="cursor-pointer"
                      aria-expanded={isOpen}
                      onClick={() => toggle(id)}
                    >
                      <DenseTableCell className="whitespace-nowrap">
                        <span className="mr-1 text-muted-foreground">{isOpen ? '▾' : '▸'}</span>
                        {renaming === t.session && t.session !== '' ? (
                          <ThreadTitleEditor
                            thread={t}
                            onDone={changed => {
                              setRenaming(null)
                              if (changed) void qc.invalidateQueries({ queryKey: ['lineage'] })
                            }}
                          />
                        ) : (
                          <>
                            {t.link != null && t.link !== '' ? (
                              <a
                                href={t.link}
                                title={`${t.session} — open thread${t.title_source === 'manual' ? ' · named by hand' : t.title_source === 'transcript' ? ' · session title' : ''}`}
                                className="text-primary hover:underline"
                                onClick={e => e.stopPropagation()}
                                target={t.link.startsWith('https:') ? '_blank' : undefined}
                                rel="noreferrer"
                              >
                                {threadName(t)}
                              </a>
                            ) : (
                              <span title={t.session}>{threadName(t)}</span>
                            )}
                            {t.session !== '' && (
                              <button
                                type="button"
                                className="ml-2 text-[var(--text-dense-meta)] text-muted-foreground hover:text-foreground disabled:opacity-40"
                                disabled={!canOperate}
                                title={canOperate ? 'Rename this thread' : 'Authenticate as operator to rename'}
                                onClick={e => {
                                  e.stopPropagation()
                                  setRenaming(t.session)
                                }}
                              >
                                ✎
                              </button>
                            )}
                            {t.title != null && t.title !== '' && (
                              <span className="ml-2 font-mono-tabular text-[var(--text-dense-meta)] text-muted-foreground">
                                {threadLabel(t.session)}
                              </span>
                            )}
                          </>
                        )}
                      </DenseTableCell>
                      <DenseTableCell>
                        <span className="flex flex-wrap gap-1">
                          {t.repos.map(r => (
                            <DenseTag key={r.repo} variant="neutral">
                              {shortRepo(r.repo)} {r.commits.length}
                            </DenseTag>
                          ))}
                        </span>
                      </DenseTableCell>
                      <DenseTableCell className={denseTableNumCell}>{t.commit_count}</DenseTableCell>
                      <DenseTableCell className={denseTableNumCell}>
                        {t.landed === t.commit_count ? (
                          t.landed
                        ) : (
                          <span className="text-warning">
                            {t.landed} / {t.commit_count}
                          </span>
                        )}
                      </DenseTableCell>
                      <DenseTableCell className={denseTableNumCell}>
                        {eligible.length === 0 ? (
                          '—'
                        ) : prodCount === eligible.length ? (
                          prodCount
                        ) : (
                          <span className="text-warning">
                            {prodCount} / {eligible.length}
                          </span>
                        )}
                      </DenseTableCell>
                      <DenseTableCell className="font-mono-tabular whitespace-nowrap">{formatTime(t.last_at)}</DenseTableCell>
                    </DenseTableRow>
                    {isOpen && (
                      <DenseTableDetailRow>
                        <DenseTableCell colSpan={6}>
                          <ThreadCommits thread={t} />
                        </DenseTableCell>
                      </DenseTableDetailRow>
                    )}
                  </Fragment>
                )
              })}
            </DenseTableBody>
          </DenseDataTable>
        )}
      </OpsSection>

      {data != null && (data.releases.length > 0 || data.releases_error != null) && (
        <OpsSection
          title="Releases"
          description={help ? "Latest recorded release per lane and environment — the exact commit each repo was built from, read from the delivery runs and kept past CI retention. Deploying runs roll out; image builds are deployed later by a pin or GitOps sync." : undefined}
          overflow="visible"
        >
          {data.releases_error != null && data.releases_error !== '' ? (
            <p className={`py-2 ${meta}`}>Release records unavailable — {data.releases_error}</p>
          ) : (
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Lane</DenseTableHead>
                  <DenseTableHead>Env</DenseTableHead>
                  <DenseTableHead>Kind</DenseTableHead>
                  <DenseTableHead>Finished</DenseTableHead>
                  <DenseTableHead>Run</DenseTableHead>
                  <DenseTableHead>Built from</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {data.releases.map(r => (
                  <DenseTableRow key={`${r.lane}/${r.env}`}>
                    <DenseTableCell>{r.lane}</DenseTableCell>
                    <DenseTableCell className="font-semibold">{r.deploys ? r.env.toUpperCase() : r.env}</DenseTableCell>
                    <DenseTableCell>
                      <DenseTag variant={r.deploys ? 'info' : 'neutral'}>{r.deploys ? 'deploy' : 'build'}</DenseTag>
                    </DenseTableCell>
                    <DenseTableCell className="font-mono-tabular whitespace-nowrap">{formatTime(r.at)}</DenseTableCell>
                    <DenseTableCell className="max-w-[260px] truncate font-mono-tabular" title={r.run}>
                      {r.run}
                    </DenseTableCell>
                    <DenseTableCell className="w-full max-w-0 truncate font-mono-tabular" title={Object.entries(r.repos).map(([k, v]) => `${k} ${v}`).join('\n')}>
                      {Object.entries(r.repos)
                        .sort(([a], [b]) => a.localeCompare(b))
                        .map(([k, v]) => `${shortRepo(k)} ${v.slice(0, 7)}`)
                        .join(' · ') || '—'}
                    </DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          )}
        </OpsSection>
      )}

      {data != null && data.coverage.length > 0 && (
        <OpsSection
          title="Coverage"
          description={
            help
              ? `Main-branch commits in the window per repo. "Agent, no thread" has a Claude co-author line but no Claude-Session trailer — made before the hooks (2026-10-06) or where they did not run. "Mirror fetched" is when Gitea last pulled the repo from GitHub; each scan asks it to fetch first (at most every 2 min) and waits up to 10 s.`
              : undefined
          }
          collapsible
          defaultCollapsed
          overflow="visible"
        >
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Repo</DenseTableHead>
                <DenseTableHead className={denseTableNumCell}>Main commits</DenseTableHead>
                <DenseTableHead className={denseTableNumCell}>With thread</DenseTableHead>
                <DenseTableHead className={denseTableNumCell}>With Change-Id</DenseTableHead>
                <DenseTableHead className={denseTableNumCell}>Agent, no thread</DenseTableHead>
                <DenseTableHead className={denseTableNumCell}>Branches moved</DenseTableHead>
                <DenseTableHead className={denseTableNumCell}>Mirror fetched</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {data.coverage.map(c => (
                <DenseTableRow key={c.repo}>
                  <DenseTableCell>{shortRepo(c.repo)}</DenseTableCell>
                  <DenseTableCell className={denseTableNumCell}>{c.main_commits}</DenseTableCell>
                  <DenseTableCell className={denseTableNumCell}>{c.with_session}</DenseTableCell>
                  <DenseTableCell className={denseTableNumCell}>{c.with_change_id}</DenseTableCell>
                  <DenseTableCell className={denseTableNumCell}>{c.agent_no_lineage}</DenseTableCell>
                  <DenseTableCell className={denseTableNumCell}>{c.branches}</DenseTableCell>
                  <DenseTableCell
                    className={`${denseTableNumCell} ${
                      (mirrorAgeMs(c.mirror_updated, data.generated_at) ?? 0) > 2 * 3_600_000 ? 'text-warning' : ''
                    }`}
                  >
                    {mirrorAgeLabel(mirrorAgeMs(c.mirror_updated, data.generated_at))}
                  </DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
          <p className={`pt-2 ${meta}`}>
            {totals.untraced} agent commit{totals.untraced === 1 ? '' : 's'} on main in this window name no thread.
          </p>
        </OpsSection>
      )}

      {data != null && data.errors.length > 0 && (
        <OpsSection title="Errors" overflow="visible">
          <ul className={`list-disc pl-5 ${meta}`}>
            {data.errors.map(e => (
              <li key={e} className="font-mono-tabular">
                {e}
              </li>
            ))}
          </ul>
        </OpsSection>
      )}
    </div>
  )
}
