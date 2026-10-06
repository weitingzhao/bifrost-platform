import { useMemo, useState, type MouseEvent, type UIEvent } from 'react'
import { DenseTag } from '@bifrost/ui'
import type { LineageGraphCommit, LineageGraphMarker, LineageRepoGraph, LineageThread } from '@/api/lineage'

/**
 * Multi-repo commit graph: one row per commit, one vertical lane per repo
 * (plus a sub-lane per open branch) — a git graph whose lanes are repos.
 * Node colour = agent thread; hollow = no thread. Rows can be laid out by
 * time, grouped by thread, by repo, or by delivery stage.
 */

const ROW = 22
const LANE = 14
const PAD = 10
const OVERSCAN = 12
const VIEW_ROWS = 28
const HEADER_H = 112

/** Distinct hues readable on dark and light surfaces (git-graph style). */
const PALETTE = ['#f472b6', '#60a5fa', '#34d399', '#fbbf24', '#a78bfa', '#f87171', '#22d3ee', '#fb923c', '#a3e635', '#e879f9']
const NO_THREAD = 'var(--color-muted-foreground, #8a8f98)'
/** Lane rails: visible on both themes without competing with thread colours. */
const RAIL = 'color-mix(in srgb, var(--color-muted-foreground, #8a8f98) 70%, transparent)'
/** Alternating repo-group bands: a repo and its branch lanes share one. */
const BAND = 'color-mix(in srgb, var(--color-muted-foreground, #8a8f98) 7%, transparent)'
/** The "no thread" pseudo-session in the thread filter. */
const NONE = ''

export type LineageView = 'time' | 'thread' | 'repo' | 'stage'

const VIEWS: { id: LineageView; label: string; hint: string }[] = [
  { id: 'time', label: 'Timeline', hint: 'Every repo in one time order — what happened when' },
  { id: 'thread', label: 'By thread', hint: 'One section per Claude session — what each one did' },
  { id: 'repo', label: 'By repo', hint: 'One section per repo — each repo’s history with its branches' },
  { id: 'stage', label: 'By stage', hint: 'Branch only → on main → STG → PROD — how far each change got' },
]

/** Delivery stages, furthest first. */
const STAGES = [
  { id: 'prod', label: 'In PROD' },
  { id: 'stg', label: 'In STG, not PROD yet' },
  { id: 'image', label: 'Image built (deployed by pin / sync)' },
  { id: 'main', label: 'On main, not released' },
  { id: 'branch', label: 'Branch only — not on main' },
] as const
type StageId = (typeof STAGES)[number]['id']

type Lane = { key: string; repo: string; branch?: string; x: number; group: number }

type CommitRow = { kind: 'commit'; lane: Lane; c: LineageGraphCommit; markers: LineageGraphMarker[] }
type GapRow = { kind: 'gap'; count: number; lanesHidden: Set<string> }
type HeaderRow = { kind: 'header'; label: string; count: number; color?: string }
type Row = CommitRow | GapRow | HeaderRow

type Ev = { lane: Lane; c: LineageGraphCommit; markers: LineageGraphMarker[]; stage: StageId | null }

export type LineageGraphProps = {
  graph: LineageRepoGraph[]
  threads: LineageThread[]
  showHelp: boolean
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

/** Click = only this one (click it again = everything); ⇧ / ⌘ / Ctrl-click = add or remove. */
function pick(prev: Set<string> | null, id: string, e: MouseEvent): Set<string> | null {
  if (e.shiftKey || e.metaKey || e.ctrlKey) {
    const next = new Set(prev ?? [])
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next.size === 0 ? null : next
  }
  return prev?.size === 1 && prev.has(id) ? null : new Set([id])
}

/** Per repo: the newest default-branch index each kind of release has built (0 = newest commit). */
function releaseFrontier(g: LineageRepoGraph) {
  const idx = new Map(g.main.map((c, i) => [c.sha, i]))
  let prod: number | undefined
  let stg: number | undefined
  let image: number | undefined
  const better = (cur: number | undefined, i: number | undefined) => (i == null ? cur : cur == null ? i : Math.min(cur, i))
  for (const m of g.markers) {
    const i = idx.get(m.sha)
    if (m.deploys && m.env === 'prod') prod = better(prod, i)
    else if (m.deploys && m.env === 'stg') stg = better(stg, i)
    else if (!m.deploys) image = better(image, i)
  }
  return { prod, stg, image }
}

export function LineageGraph({ graph, threads, showHelp }: LineageGraphProps) {
  const active = useMemo(() => graph.filter(g => g.main.length > 0 || g.branches.length > 0), [graph])
  const [repoSel, setRepoSel] = useState<Set<string> | null>(null)
  const [threadSel, setThreadSel] = useState<Set<string> | null>(null)
  const [view, setView] = useState<LineageView>('time')
  const [threadedOnly, setThreadedOnly] = useState(false)
  const [allReleases, setAllReleases] = useState(false)
  const [scrollTop, setScrollTop] = useState(0)

  const colorOf = useMemo(() => {
    const m = new Map<string, string>()
    threads.filter(t => t.session !== '').forEach((t, i) => m.set(t.session, PALETTE[i % PALETTE.length]))
    return (session?: string) => (session != null && session !== '' ? (m.get(session) ?? PALETTE[0]) : NO_THREAD)
  }, [threads])

  const titleOf = useMemo(() => {
    const m = new Map(threads.map(t => [t.session, t.title ?? '']))
    return (session?: string) => {
      if (session == null || session === '') return 'No thread'
      const title = m.get(session)
      return title != null && title !== '' ? title : threadLabel(session)
    }
  }, [threads])

  // counts for the chips: commits per repo and per thread in the window
  const counts = useMemo(() => {
    const byThread = new Map<string, number>()
    const byRepo = new Map<string, number>()
    for (const g of active) {
      const cs = [...g.main, ...g.branches.flatMap(b => b.commits)]
      byRepo.set(g.repo, cs.length)
      for (const c of cs) byThread.set(c.session ?? NONE, (byThread.get(c.session ?? NONE) ?? 0) + 1)
    }
    return { byThread, byRepo }
  }, [active])

  const threadChips = useMemo(
    () => [
      ...threads.filter(t => t.session !== '' && (counts.byThread.get(t.session) ?? 0) > 0).map(t => t.session),
      ...((counts.byThread.get(NONE) ?? 0) > 0 ? [NONE] : []),
    ],
    [threads, counts],
  )

  // every event that passes the repo and thread filters
  const events = useMemo(() => {
    const evs: (Omit<Ev, 'lane'> & { repo: string; branch?: string })[] = []
    for (const g of active) {
      if (repoSel != null && !repoSel.has(g.repo)) continue
      const bySha = new Map<string, LineageGraphMarker[]>()
      const seen = new Set<string>()
      for (const m of g.markers) {
        const k = `${m.lane}/${m.env}`
        if (!allReleases && seen.has(k)) continue // markers come newest first
        seen.add(k)
        bySha.set(m.sha, [...(bySha.get(m.sha) ?? []), m])
      }
      const f = releaseFrontier(g)
      g.main.forEach((c, i) => {
        if (threadSel != null && !threadSel.has(c.session ?? NONE)) return
        const stage: StageId =
          f.prod != null && i >= f.prod ? 'prod' : f.stg != null && i >= f.stg ? 'stg' : f.image != null && i >= f.image ? 'image' : 'main'
        evs.push({ repo: g.repo, c, markers: bySha.get(c.sha) ?? [], stage })
      })
      for (const b of g.branches)
        for (const c of b.commits) {
          if (threadSel != null && !threadSel.has(c.session ?? NONE)) continue
          evs.push({ repo: g.repo, branch: b.name, c, markers: [], stage: c.landed_by ? null : 'branch' })
        }
    }
    return evs
  }, [active, repoSel, threadSel, allReleases])

  // lanes: only repos (and branches) that still have events, so a filter narrows the graph too
  const lanes = useMemo(() => {
    const used = new Set(events.map(e => (e.branch != null ? `${e.repo}::${e.branch}` : e.repo)))
    const out: Lane[] = []
    let group = 0
    for (const g of active) {
      const keys = [g.repo, ...g.branches.map(b => `${g.repo}::${b.name}`)].filter(k => used.has(k))
      if (keys.length === 0) continue
      if (!keys.includes(g.repo)) keys.unshift(g.repo) // a branch lane keeps its repo rail beside it
      for (const k of keys) {
        const branch = k.includes('::') ? k.slice(k.indexOf('::') + 2) : undefined
        out.push({ key: k, repo: g.repo, branch, x: 0, group })
      }
      group++
    }
    out.forEach((l, i) => (l.x = PAD + i * LANE))
    return out
  }, [active, events])

  const groups = useMemo(() => {
    const m = new Map<string, { repo: string; x0: number; x1: number; group: number }>()
    for (const l of lanes) {
      const g = m.get(l.repo)
      if (g == null) m.set(l.repo, { repo: l.repo, x0: l.x, x1: l.x, group: l.group })
      else g.x1 = l.x
    }
    return [...m.values()]
  }, [lanes])

  const { rows, span, prodRow, links } = useMemo(() => {
    const laneOf = new Map(lanes.map(l => [l.key, l]))
    const evs: Ev[] = []
    for (const e of events) {
      const lane = laneOf.get(e.branch != null ? `${e.repo}::${e.branch}` : e.repo)
      if (lane != null) evs.push({ lane, c: e.c, markers: e.markers, stage: e.stage })
    }
    const byTime = (a: Ev, b: Ev) => Date.parse(b.c.at) - Date.parse(a.c.at)

    // sections for the grouped views; one unlabelled section for the timeline
    let sections: { label: string; color?: string; evs: Ev[] }[]
    if (view === 'thread') {
      const m = new Map<string, Ev[]>()
      for (const e of evs) m.set(e.c.session ?? NONE, [...(m.get(e.c.session ?? NONE) ?? []), e])
      sections = [...m.entries()]
        .map(([s, list]) => ({ label: titleOf(s), color: colorOf(s), evs: list.sort(byTime) }))
        .sort((a, b) => (a.color === NO_THREAD ? 1 : b.color === NO_THREAD ? -1 : Date.parse(b.evs[0].c.at) - Date.parse(a.evs[0].c.at)))
    } else if (view === 'repo') {
      sections = groups.map(g => ({ label: shortRepo(g.repo), evs: evs.filter(e => e.lane.repo === g.repo).sort(byTime) }))
    } else if (view === 'stage') {
      sections = STAGES.map(st => ({ label: st.label, evs: evs.filter(e => e.stage === st.id).sort(byTime) }))
    } else {
      sections = [{ label: '', evs: evs.sort(byTime) }]
    }

    const rows: Row[] = []
    const shaRow = new Map<string, number>()
    for (const sec of sections) {
      if (sec.evs.length === 0) continue
      if (view !== 'time') rows.push({ kind: 'header', label: sec.label, count: sec.evs.length, color: sec.color })
      for (const e of sec.evs) {
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
    }

    // rails: each lane from its first to its last row (the main rail runs on past
    // the window in the timeline: history continues)
    const span = new Map<string, [number, number]>()
    rows.forEach((r, i) => {
      const keys = r.kind === 'commit' ? [r.lane.key] : r.kind === 'gap' ? [...r.lanesHidden] : []
      for (const k of keys) {
        const s = span.get(k)
        span.set(k, s == null ? [i, i] : [s[0], i])
      }
    })
    if (view === 'time') for (const l of lanes) if (l.branch == null && span.has(l.key)) span.set(l.key, [span.get(l.key)![0], rows.length])

    // timeline only: dashed rail above the newest PROD build; fork and landed connectors
    const prodRow = new Map<string, number>()
    type Link = { key: string; x1: number; r1: number; x2: number; r2: number; kind: 'fork' | 'landed' }
    const links: Link[] = []
    if (view === 'time' || view === 'repo') {
      for (const g of active) {
        const f = releaseFrontier(g)
        if (view === 'time' && f.prod != null) {
          const r = shaRow.get(`${g.repo}:${g.main[f.prod].sha}`)
          if (r != null) prodRow.set(g.repo, r)
        }
        const main = laneOf.get(g.repo)
        if (main == null) continue
        for (const b of g.branches) {
          const lane = laneOf.get(`${g.repo}::${b.name}`)
          const oldest = b.commits[b.commits.length - 1]
          if (lane == null || oldest == null) continue
          const from = shaRow.get(`${g.repo}:${oldest.sha}`)
          if (from == null) continue
          const fork = b.fork_sha != null ? shaRow.get(`${g.repo}:${b.fork_sha}`) : undefined
          const end = rows.length
          links.push({ key: `fork:${lane.key}`, x1: lane.x, r1: from, x2: main.x, r2: fork ?? end, kind: 'fork' })
          for (const c of b.commits) {
            if (!c.landed_sha) continue
            const at = shaRow.get(`${g.repo}:${c.sha}`)
            const to = shaRow.get(`${g.repo}:${c.landed_sha}`)
            if (at != null && to != null) links.push({ key: `land:${lane.key}:${c.sha}`, x1: lane.x, r1: at, x2: main.x, r2: to, kind: 'landed' })
          }
        }
      }
    }
    return { rows, span, prodRow, links }
  }, [events, lanes, groups, active, view, threadedOnly, titleOf, colorOf])

  const viewH = VIEW_ROWS * ROW
  const first = Math.max(0, Math.floor(scrollTop / ROW) - OVERSCAN)
  const last = Math.min(rows.length, Math.ceil((scrollTop + viewH) / ROW) + OVERSCAN)
  const graphW = PAD * 2 + Math.max(lanes.length - 1, 0) * LANE

  const chip = (on: boolean) =>
    `inline-flex h-6 items-center gap-1 rounded-[var(--control-radius)] px-2 text-[var(--text-dense-caption)] ${
      on ? 'bg-[var(--control-fill-hover)] text-foreground' : 'bg-[var(--control-fill)] text-muted-foreground opacity-60'
    }`
  const resetBtn = (onClick: () => void, on: boolean) => (
    <button type="button" className={chip(on)} onClick={onClick} title="Show all">
      All
    </button>
  )

  return (
    <div className="flex flex-col gap-2">
      {/* view modes */}
      <div className="flex flex-wrap items-center gap-1">
        <span className="mr-1 text-[var(--text-dense-meta)] text-muted-foreground">View</span>
        {VIEWS.map(v => (
          <button
            key={v.id}
            type="button"
            title={v.hint}
            className={`inline-flex h-6 items-center rounded-[var(--control-radius)] px-2 text-[var(--text-dense-caption)] ${
              view === v.id ? 'bg-primary text-primary-foreground' : 'bg-[var(--control-fill)] text-foreground hover:bg-[var(--control-fill-hover)]'
            }`}
            onClick={() => {
              setView(v.id)
              setScrollTop(0)
            }}
          >
            {v.label}
          </button>
        ))}
        <span className="mx-2 h-4 w-px bg-[var(--control-fill-hover)]" />
        <label className="inline-flex items-center gap-1 text-[var(--text-dense-caption)] text-muted-foreground">
          <input type="checkbox" checked={threadedOnly} onChange={e => setThreadedOnly(e.target.checked)} />
          Fold commits without a thread
        </label>
        <label className="ml-2 inline-flex items-center gap-1 text-[var(--text-dense-caption)] text-muted-foreground">
          <input type="checkbox" checked={allReleases} onChange={e => setAllReleases(e.target.checked)} />
          Every release
        </label>
      </div>

      {/* repo filter */}
      <div className="flex flex-wrap items-center gap-1">
        <span className="mr-1 w-[52px] text-[var(--text-dense-meta)] text-muted-foreground">Repos</span>
        {resetBtn(() => setRepoSel(null), repoSel == null)}
        {active.map(g => (
          <button
            key={g.repo}
            type="button"
            className={chip(repoSel == null || repoSel.has(g.repo))}
            title="Click: only this repo (again: all) · ⇧-click: add / remove"
            onClick={e => setRepoSel(prev => pick(prev, g.repo, e))}
          >
            {shortRepo(g.repo)} <span className="font-mono-tabular text-muted-foreground">{counts.byRepo.get(g.repo)}</span>
          </button>
        ))}
      </div>

      {/* thread filter */}
      <div className="flex flex-wrap items-center gap-1">
        <span className="mr-1 w-[52px] text-[var(--text-dense-meta)] text-muted-foreground">Threads</span>
        {resetBtn(() => setThreadSel(null), threadSel == null)}
        {threadChips.map(s => (
          <button
            key={s || 'none'}
            type="button"
            title={`${s || 'commits without a thread'} — click: only this thread (again: all) · ⇧-click: add / remove`}
            className={chip(threadSel == null || threadSel.has(s))}
            onClick={e => setThreadSel(prev => pick(prev, s, e))}
          >
            {s === NONE ? (
              <svg width="10" height="10" aria-hidden>
                <circle cx="5" cy="5" r="3.5" fill="none" stroke={NO_THREAD} strokeWidth="1.5" />
              </svg>
            ) : (
              <span className="inline-block h-2.5 w-2.5 rounded-full" style={{ background: colorOf(s) }} />
            )}
            <span className="max-w-[220px] truncate">{titleOf(s)}</span>
            <span className="text-muted-foreground">{counts.byThread.get(s)}</span>
          </button>
        ))}
      </div>

      {showHelp && (
        <p className="text-[var(--text-dense-meta)] text-muted-foreground">
          Click a chip to show only that repo or thread (click it again for all); ⇧-click adds or removes. Hollow dot = no thread.
          A ring marks the commit a release built. Timeline: a lane is dashed above its latest PROD build (landed, not in PROD
          yet); a yellow curve shows where a branch left main; a dotted green line points from a branch change to the main
          commit that carries it.
        </p>
      )}

      {rows.length === 0 ? (
        <p className="py-4 text-center text-[var(--text-dense-meta)] text-muted-foreground">No commits match these filters.</p>
      ) : (
        <>
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
                title={l.branch != null ? `${l.repo} ⎇ ${l.branch} (changes not on ${shortRepo(l.repo)}'s main)` : `${l.repo} — default branch`}
                className={`absolute bottom-1 truncate text-[10px] leading-[14px] ${l.branch != null ? 'text-warning' : 'font-semibold text-foreground'}`}
                style={{ left: l.x - 7, width: 14, maxHeight: HEADER_H - 6, writingMode: 'vertical-rl', transform: 'rotate(180deg)' }}
              >
                {l.branch != null ? `↳ ${l.branch.replace(/^(claude|fix|chore|td-batch|debt|feat)\//, '')}` : shortRepo(l.repo)}
              </span>
            ))}
            <span className="absolute bottom-0 text-[var(--text-dense-meta)] text-muted-foreground" style={{ left: graphW + 4 }}>
              {VIEWS.find(v => v.id === view)?.hint} · {rows.length} rows
            </span>
          </div>

          <div
            className="relative overflow-y-auto rounded-[var(--control-radius)] bg-[var(--field-fill)]"
            style={{ height: Math.min(viewH, rows.length * ROW) + 2 }}
            onScroll={(e: UIEvent<HTMLDivElement>) => setScrollTop(e.currentTarget.scrollTop)}
          >
            <div style={{ height: rows.length * ROW, position: 'relative' }}>
              <svg width={graphW} height={(last - first) * ROW} style={{ position: 'absolute', top: first * ROW, left: 0 }} aria-hidden>
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
                  if (r.kind !== 'commit') return null
                  const y = k * ROW + ROW / 2
                  const color = colorOf(r.c.session)
                  const hollow = !r.c.session
                  const prod = r.markers.some(m => m.deploys && m.env === 'prod')
                  return (
                    <g key={`${r.lane.key}:${r.c.sha}`}>
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
                if (r.kind === 'header')
                  return (
                    <div
                      key={`h-${first + k}`}
                      className="absolute flex items-center gap-2 border-b border-dashed border-[var(--table-rule)] text-[var(--text-dense-caption)] font-semibold"
                      style={{ top, height: ROW, left: graphW + 4, right: 8 }}
                    >
                      {r.color != null && <span className="inline-block h-2.5 w-2.5 rounded-full" style={{ background: r.color }} />}
                      <span className="truncate">{r.label}</span>
                      <span className="font-mono-tabular text-muted-foreground">{r.count}</span>
                    </div>
                  )
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
                    style={{ top, height: ROW, left: graphW + 4, right: 0 }}
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
                      <button
                        type="button"
                        className="inline-flex shrink-0 items-center gap-1 pr-2"
                        style={{ color: colorOf(c.session) }}
                        title="Show only this thread"
                        onClick={e => setThreadSel(prev => pick(prev, c.session ?? NONE, e))}
                      >
                        ● <span className="max-w-[200px] truncate">{titleOf(c.session)}</span>
                      </button>
                    )}
                  </div>
                )
              })}
            </div>
          </div>
        </>
      )}
    </div>
  )
}
