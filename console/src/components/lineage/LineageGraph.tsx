import { useMemo, useRef, useState, type UIEvent } from 'react'
import { DenseTag } from '@bifrost/ui'
import type { LineageGraphCommit, LineageGraphMarker, LineageRepoGraph, LineageThread } from '@/api/lineage'

/**
 * Multi-repo commit graph: one row per commit, all repos merged in time order,
 * one vertical lane per repo (plus a sub-lane per open branch) — a git graph
 * whose lanes are repos. Node colour = agent thread; hollow = no thread.
 * Release badges sit on the commit a release built; a repo's lane is dashed
 * above its latest PROD build (landed, not in PROD yet).
 */

const ROW = 22
const LANE = 14
const PAD = 10
const OVERSCAN = 12
const VIEW_ROWS = 28

/** Distinct hues readable on dark and light surfaces (git-graph style). */
const PALETTE = ['#f472b6', '#60a5fa', '#34d399', '#fbbf24', '#a78bfa', '#f87171', '#22d3ee', '#fb923c', '#a3e635', '#e879f9']
const NO_THREAD = 'var(--color-muted-foreground, #8a8f98)'
/** Lane rails: visible on both themes without competing with thread colours. */
const RAIL = 'color-mix(in srgb, var(--color-muted-foreground, #8a8f98) 70%, transparent)'
const HEADER_H = 112
/** Alternating repo-group bands: a repo and its branch lanes share one. */
const BAND = 'color-mix(in srgb, var(--color-muted-foreground, #8a8f98) 7%, transparent)'

type Lane = { key: string; repo: string; branch?: string; x: number; group: number }

type CommitRow = {
  kind: 'commit'
  lane: Lane
  c: LineageGraphCommit
  markers: LineageGraphMarker[]
}
type GapRow = { kind: 'gap'; count: number; lanesHidden: Set<string> }
type Row = CommitRow | GapRow

export type LineageGraphProps = {
  graph: LineageRepoGraph[]
  threads: LineageThread[]
}

function shortRepo(repo: string): string {
  return repo.replace(/^bifrost-/, '')
}

function threadLabel(session: string): string {
  if (session.startsWith('local_')) return session.slice(0, 14)
  const cloud = /session_[A-Za-z0-9]+/.exec(session)
  return cloud != null ? `cloud ${cloud[0].slice(0, 16)}` : session.slice(0, 20)
}

function hhmm(iso: string): string {
  const d = new Date(iso)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

function markerLabel(m: LineageGraphMarker): string {
  return m.deploys ? `${m.lane} ${m.env.toUpperCase()}` : `${m.lane} ${m.env}`
}

export function LineageGraph({ graph, threads }: LineageGraphProps) {
  const active = useMemo(() => graph.filter(g => g.main.length > 0 || g.branches.length > 0), [graph])
  const [hiddenRepos, setHiddenRepos] = useState<Set<string>>(new Set())
  const [threadedOnly, setThreadedOnly] = useState(false)
  const [allReleases, setAllReleases] = useState(false)
  const [focus, setFocus] = useState<string | null>(null)
  const [scrollTop, setScrollTop] = useState(0)
  const scroller = useRef<HTMLDivElement>(null)

  // thread colours: most recent thread first, stable within one response
  const colorOf = useMemo(() => {
    const m = new Map<string, string>()
    threads
      .filter(t => t.session !== '')
      .forEach((t, i) => m.set(t.session, PALETTE[i % PALETTE.length]))
    return (session?: string) => (session != null && session !== '' ? (m.get(session) ?? PALETTE[0]) : NO_THREAD)
  }, [threads])

  const visible = useMemo(() => active.filter(g => !hiddenRepos.has(g.repo)), [active, hiddenRepos])

  const lanes = useMemo(() => {
    const out: Lane[] = []
    visible.forEach((g, group) => {
      out.push({ key: g.repo, repo: g.repo, x: 0, group })
      for (const b of g.branches) out.push({ key: `${g.repo}::${b.name}`, repo: g.repo, branch: b.name, x: 0, group })
    })
    out.forEach((l, i) => (l.x = PAD + i * LANE))
    return out
  }, [visible])

  /** Repo groups: x extent of a repo's lanes (main + branches), for bands and headers. */
  const groups = useMemo(() => {
    const m = new Map<string, { repo: string; x0: number; x1: number; group: number }>()
    for (const l of lanes) {
      const g = m.get(l.repo)
      if (g == null) m.set(l.repo, { repo: l.repo, x0: l.x, x1: l.x, group: l.group })
      else g.x1 = l.x
    }
    return [...m.values()]
  }, [lanes])

  const titleOf = useMemo(() => {
    const m = new Map(threads.map(t => [t.session, t.title ?? '']))
    return (session?: string) => {
      if (session == null || session === '') return ''
      const title = m.get(session)
      return title != null && title !== '' ? title : threadLabel(session)
    }
  }, [threads])

  const { rows, span, prodRow, links } = useMemo(() => {
    const laneOf = new Map(lanes.map(l => [l.key, l]))
    type Ev = { lane: Lane; c: LineageGraphCommit; markers: LineageGraphMarker[] }
    const evs: Ev[] = []
    const latestProd = new Map<string, string>() // repo -> sha of its newest PROD deploy build
    for (const g of visible) {
      const bySha = new Map<string, LineageGraphMarker[]>()
      const seen = new Set<string>()
      for (const m of g.markers) {
        const k = `${m.lane}/${m.env}`
        if (m.deploys && m.env === 'prod' && !latestProd.has(g.repo)) latestProd.set(g.repo, m.sha)
        if (!allReleases && seen.has(k)) continue // markers come newest first
        seen.add(k)
        bySha.set(m.sha, [...(bySha.get(m.sha) ?? []), m])
      }
      const main = laneOf.get(g.repo)
      if (main == null) continue
      for (const c of g.main) evs.push({ lane: main, c, markers: bySha.get(c.sha) ?? [] })
      for (const b of g.branches) {
        const lane = laneOf.get(`${g.repo}::${b.name}`)
        if (lane != null) for (const c of b.commits) evs.push({ lane, c, markers: [] })
      }
    }
    evs.sort((a, b) => Date.parse(b.c.at) - Date.parse(a.c.at))

    const rows: Row[] = []
    const shaRow = new Map<string, number>()
    for (const e of evs) {
      const hide = threadedOnly && !e.c.session && e.markers.length === 0 && e.lane.branch == null
      if (hide) {
        const last = rows[rows.length - 1]
        if (last?.kind === 'gap') {
          last.count++
          last.lanesHidden.add(e.lane.key)
        } else rows.push({ kind: 'gap', count: 1, lanesHidden: new Set([e.lane.key]) })
      } else rows.push({ kind: 'commit', lane: e.lane, c: e.c, markers: e.markers })
      shaRow.set(`${e.lane.repo}:${e.c.sha}`, rows.length - 1)
    }
    // each lane is drawn from its first to its last row
    const span = new Map<string, [number, number]>()
    rows.forEach((r, i) => {
      const keys = r.kind === 'commit' ? [r.lane.key] : [...r.lanesHidden]
      for (const k of keys) {
        const s = span.get(k)
        span.set(k, s == null ? [i, i] : [s[0], i])
      }
    })
    // main lanes run through the whole window: the history continues past it
    for (const l of lanes) if (l.branch == null && span.has(l.key)) span.set(l.key, [span.get(l.key)![0], rows.length])
    const prodRow = new Map<string, number>()
    for (const [repo, sha] of latestProd) {
      const r = shaRow.get(`${repo}:${sha}`)
      if (r != null) prodRow.set(repo, r)
    }
    // Connectors: a branch leaves its repo's main lane at the fork commit (or below
    // the window when it forked earlier); a branch change that landed points at
    // the main commit that carries it.
    type Link = { key: string; x1: number; r1: number; x2: number; r2: number; kind: 'fork' | 'landed' }
    const links: Link[] = []
    for (const g of visible) {
      const main = laneOf.get(g.repo)
      if (main == null) continue
      for (const b of g.branches) {
        const lane = laneOf.get(`${g.repo}::${b.name}`)
        const oldest = b.commits[b.commits.length - 1]
        if (lane == null || oldest == null) continue
        const from = shaRow.get(`${g.repo}:${oldest.sha}`)
        if (from == null) continue
        const fork = b.fork_sha != null ? shaRow.get(`${g.repo}:${b.fork_sha}`) : undefined
        links.push({ key: `fork:${lane.key}`, x1: lane.x, r1: from, x2: main.x, r2: fork ?? rows.length, kind: 'fork' })
        for (const c of b.commits) {
          if (!c.landed_sha) continue
          const at = shaRow.get(`${g.repo}:${c.sha}`)
          const to = shaRow.get(`${g.repo}:${c.landed_sha}`)
          if (at != null && to != null) links.push({ key: `land:${lane.key}:${c.sha}`, x1: lane.x, r1: at, x2: main.x, r2: to, kind: 'landed' })
        }
      }
    }
    return { rows, span, prodRow, links }
  }, [visible, lanes, threadedOnly, allReleases])

  const viewH = VIEW_ROWS * ROW
  const first = Math.max(0, Math.floor(scrollTop / ROW) - OVERSCAN)
  const last = Math.min(rows.length, Math.ceil((scrollTop + viewH) / ROW) + OVERSCAN)
  const graphW = PAD * 2 + Math.max(lanes.length - 1, 0) * LANE

  const dim = (session?: string) => focus != null && session !== focus
  const counts = useMemo(() => {
    const m = new Map<string, number>()
    for (const g of visible) for (const c of [...g.main, ...g.branches.flatMap(b => b.commits)]) if (c.session) m.set(c.session, (m.get(c.session) ?? 0) + 1)
    return m
  }, [visible])

  const toggleRepo = (repo: string) =>
    setHiddenRepos(prev => {
      const next = new Set(prev)
      if (next.has(repo)) next.delete(repo)
      else next.add(repo)
      return next
    })

  const chip = (on: boolean) =>
    `inline-flex h-6 items-center gap-1 rounded-[var(--control-radius)] px-2 text-[var(--text-dense-caption)] ${
      on ? 'bg-[var(--control-fill-hover)] text-foreground' : 'bg-[var(--control-fill)] text-muted-foreground line-through'
    }`

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-1">
        {active.map(g => (
          <button key={g.repo} type="button" className={chip(!hiddenRepos.has(g.repo))} onClick={() => toggleRepo(g.repo)}>
            {shortRepo(g.repo)} <span className="font-mono-tabular text-muted-foreground">{g.main.length}</span>
          </button>
        ))}
        <span className="mx-1 h-4 w-px bg-[var(--border)]" />
        <label className="inline-flex items-center gap-1 text-[var(--text-dense-caption)] text-muted-foreground">
          <input type="checkbox" checked={threadedOnly} onChange={e => setThreadedOnly(e.target.checked)} />
          Threaded commits only
        </label>
        <label className="ml-2 inline-flex items-center gap-1 text-[var(--text-dense-caption)] text-muted-foreground">
          <input type="checkbox" checked={allReleases} onChange={e => setAllReleases(e.target.checked)} />
          Every release (default: latest per env)
        </label>
      </div>

      <div className="flex flex-wrap items-center gap-1">
        {threads
          .filter(t => t.session !== '' && (counts.get(t.session) ?? 0) > 0)
          .map(t => (
            <button
              key={t.session}
              type="button"
              title={`${t.session} — click to highlight`}
              onClick={() => setFocus(f => (f === t.session ? null : t.session))}
              className={`inline-flex h-6 items-center gap-1 rounded-[var(--control-radius)] px-2 text-[var(--text-dense-caption)] ${
                focus === t.session ? 'bg-[var(--control-fill-hover)]' : 'bg-[var(--control-fill)]'
              } ${focus != null && focus !== t.session ? 'opacity-40' : ''}`}
            >
              <span className="inline-block h-2.5 w-2.5 rounded-full" style={{ background: colorOf(t.session) }} />
              <span className="max-w-[220px] truncate">{titleOf(t.session)}</span>
              <span className="text-muted-foreground">{counts.get(t.session)}</span>
            </button>
          ))}
        <span className="inline-flex items-center gap-1 text-[var(--text-dense-caption)] text-muted-foreground">
          <svg width="12" height="12" aria-hidden>
            <circle cx="6" cy="6" r="4" fill="none" stroke={NO_THREAD} strokeWidth="1.5" />
          </svg>
          no thread · dashed lane = landed, not in PROD yet · yellow curve = branch forks off · dotted green = branch change landed here
        </span>
      </div>

      {rows.length > 0 && (
        <div className="relative" style={{ height: HEADER_H }} aria-label="Lanes">
          {groups.map(g =>
            g.group % 2 === 0 ? (
              <span
                key={`hband:${g.repo}`}
                className="absolute top-0 bottom-0 rounded-t-[4px]"
                style={{ left: g.x0 - LANE / 2, width: g.x1 - g.x0 + LANE, background: BAND }}
              />
            ) : null,
          )}
          {lanes.map(l => (
            <span
              key={l.key}
              title={l.branch != null ? `${l.repo} ⎇ ${l.branch} (branch with changes not on ${shortRepo(l.repo)}'s main)` : `${l.repo} — default branch`}
              className={`absolute bottom-1 truncate text-[10px] leading-[14px] ${
                l.branch != null ? 'text-warning' : 'font-semibold text-foreground'
              }`}
              style={{ left: l.x - 7, width: 14, maxHeight: HEADER_H - 6, writingMode: 'vertical-rl', transform: 'rotate(180deg)' }}
            >
              {l.branch != null ? `↳ ${l.branch.replace(/^(claude|fix|chore|td-batch|debt|feat)\//, '')}` : shortRepo(l.repo)}
            </span>
          ))}
          <span className="absolute bottom-0 text-[var(--text-dense-meta)] text-muted-foreground" style={{ left: graphW + 4 }}>
            newest first · {rows.length} rows · hover a row for detail, click to highlight its thread
          </span>
        </div>
      )}

      {rows.length === 0 ? (
        <p className="py-4 text-center text-[var(--text-dense-meta)] text-muted-foreground">No commits in this window.</p>
      ) : (
        <div
          ref={scroller}
          className="relative overflow-y-auto rounded-[var(--control-radius)] bg-[var(--field-fill)]"
          style={{ height: Math.min(viewH, rows.length * ROW) + 2 }}
          onScroll={(e: UIEvent<HTMLDivElement>) => setScrollTop(e.currentTarget.scrollTop)}
        >
          <div style={{ height: rows.length * ROW, position: 'relative' }}>
            {/* lane header over the graph column */}
            <svg
              width={graphW}
              height={(last - first) * ROW}
              style={{ position: 'absolute', top: first * ROW, left: 0 }}
              aria-hidden
            >
              {groups.map(g =>
                g.group % 2 === 0 ? (
                  <rect key={`band:${g.repo}`} x={g.x0 - LANE / 2} y={0} width={g.x1 - g.x0 + LANE} height={(last - first) * ROW} fill={BAND} />
                ) : null,
              )}
              {links.map(k => {
                if (Math.max(k.r1, k.r2) < first || Math.min(k.r1, k.r2) > last) return null
                const y1 = (k.r1 - first) * ROW + ROW / 2
                const y2 = (k.r2 - first) * ROW + ROW / 2
                const mid = (y1 + y2) / 2
                return (
                  <path
                    key={k.key}
                    d={`M ${k.x1} ${y1} C ${k.x1} ${mid}, ${k.x2} ${mid}, ${k.x2} ${y2}`}
                    fill="none"
                    stroke={k.kind === 'fork' ? 'var(--color-warning)' : 'var(--color-success)'}
                    strokeWidth={1.5}
                    strokeDasharray={k.kind === 'landed' ? '2 3' : undefined}
                    opacity={0.85}
                  />
                )
              })}
              {lanes.map(l => {
                const s = span.get(l.key)
                if (s == null) return null
                const a = Math.max(s[0], first)
                const b = Math.min(s[1], last)
                if (a >= b) return null
                const y = (i: number) => (i - first) * ROW + ROW / 2
                const pr = l.branch == null ? prodRow.get(l.repo) : undefined
                const stroke = l.branch == null ? RAIL : 'var(--color-warning)'
                if (pr == null || pr <= a)
                  return <line key={l.key} x1={l.x} x2={l.x} y1={y(a)} y2={y(b)} stroke={stroke} strokeWidth={2} strokeDasharray={l.branch != null ? '3 3' : undefined} />
                const mid = Math.min(pr, b)
                return (
                  <g key={l.key}>
                    <line x1={l.x} x2={l.x} y1={y(a)} y2={y(mid)} stroke={stroke} strokeWidth={2} strokeDasharray="4 3" />
                    {mid < b && <line x1={l.x} x2={l.x} y1={y(mid)} y2={y(b)} stroke={stroke} strokeWidth={2} />}
                  </g>
                )
              })}
              {rows.slice(first, last).map((r, k) => {
                const y = k * ROW + ROW / 2
                if (r.kind === 'gap') return null
                const color = colorOf(r.c.session)
                const hollow = !r.c.session
                const prod = r.markers.some(m => m.deploys && m.env === 'prod')
                return (
                  <g key={`${r.lane.key}:${r.c.sha}`} opacity={dim(r.c.session) ? 0.2 : 1}>
                    {r.markers.length > 0 && (
                      <circle cx={r.lane.x} cy={y} r={7} fill="none" stroke={prod ? 'var(--color-success)' : 'var(--color-info)'} strokeWidth={1.5} />
                    )}
                    <circle
                      cx={r.lane.x}
                      cy={y}
                      r={r.lane.branch != null ? 3.5 : 4.5}
                      fill={hollow ? 'var(--surface, transparent)' : color}
                      stroke={color}
                      strokeWidth={hollow ? 1.5 : 1}
                    />
                  </g>
                )
              })}
            </svg>

            {rows.slice(first, last).map((r, k) => {
              const top = (first + k) * ROW
              if (r.kind === 'gap')
                return (
                  <div
                    key={`gap-${first + k}`}
                    className="absolute flex items-center text-[var(--text-dense-meta)] text-muted-foreground"
                    style={{ top, height: ROW, left: graphW + 4, right: 8 }}
                  >
                    ··· {r.count} commit{r.count === 1 ? '' : 's'} without a thread
                  </div>
                )
              const c = r.c
              const branchState =
                r.lane.branch == null ? null : c.landed_by ? `landed via ${c.landed_by === 'change_id' ? 'Change-Id' : 'subject'}` : 'not on main'
              return (
                <div
                  key={`${r.lane.key}:${c.sha}`}
                  className="absolute flex min-w-0 items-center gap-2 whitespace-nowrap text-[var(--text-dense-meta)] hover:bg-[var(--control-fill)]"
                  style={{ top, height: ROW, left: graphW + 4, right: 0, opacity: dim(c.session) ? 0.3 : 1 }}
                  title={[
                    `${c.sha}  ${r.lane.repo}${r.lane.branch != null ? ` @ ${r.lane.branch}` : ''}`,
                    c.subject,
                    c.session ? `thread ${titleOf(c.session)} (${c.session})` : c.agent ? 'agent commit, no thread (before the hooks)' : 'no thread',
                    c.change_id ? `Change-Id ${c.change_id}` : '',
                    branchState ?? '',
                    ...r.markers.map(m => `${markerLabel(m)} · ${m.run} · ${hhmm(m.at)}`),
                  ]
                    .filter(Boolean)
                    .join('\n')}
                  onClick={() => c.session && setFocus(f => (f === c.session ? null : (c.session ?? null)))}
                >
                  <span className="font-mono-tabular text-muted-foreground">{hhmm(c.at)}</span>
                  <span className="w-[120px] shrink-0 truncate text-muted-foreground">
                    {shortRepo(r.lane.repo)}
                    {r.lane.branch != null ? ` ⎇ ${r.lane.branch}` : ''}
                  </span>
                  <span className="font-mono-tabular text-muted-foreground">{c.sha.slice(0, 7)}</span>
                  <span className="min-w-0 flex-1 truncate">{c.subject}</span>
                  {branchState != null && <DenseTag variant={c.landed_by ? 'neutral' : 'warning'}>{branchState}</DenseTag>}
                  {r.markers.map(m => (
                    <DenseTag key={m.run} variant={m.deploys ? (m.env === 'prod' ? 'success' : 'info') : 'neutral'}>
                      {markerLabel(m)} {hhmm(m.at).slice(6)}
                    </DenseTag>
                  ))}
                  {c.session && (
                    <span className="inline-flex shrink-0 items-center gap-1 pr-2 font-mono-tabular" style={{ color: colorOf(c.session) }}>
                      ● <span className="max-w-[200px] truncate">{titleOf(c.session)}</span>
                    </span>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}
