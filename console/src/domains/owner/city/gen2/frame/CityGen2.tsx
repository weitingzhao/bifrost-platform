import * as Phaser from 'phaser'
import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { browserArrangements, type ArrangementStore } from './arrangementStore'
import { arrange, move, toArrangement, type Arrangement } from './grid'
import { CORNERS, lightOf, NEXT, type LightChoice } from './light'
import type { CityContent, Panel } from './content'
import { PlaceScene, type Shown } from './placeScene'
import { here, resolve, trail, type Path, type Place } from './places'
import { frameOf, NO_INSETS, type Insets, type Rect } from './viewport'
import './cityGen2.css'

/**
 * The second generation's frame, greybox (the Owner, 2026-11-19): the game
 * is laid out like the Console, so the two looks can swap.
 *
 *   Menu      → the bar along the bottom: one button per district
 *   Page      → the place on screen; entering a building is opening a page
 *   Summary   → the top-left corner (on a phone, one line; tap it for more)
 *   Section   → the blocks inside a place
 *   Button    → a block you can tap
 *   Popup     → the card of a room: on the right, or a bottom sheet on a phone
 *   Back      → the minimap (the place you came from, shrunk), the trail,
 *               the browser's Back, or Esc
 *
 * Every rectangle comes from viewport.ts for the screen's size, the same one
 * the canvas uses for its cameras. Every place has an address.
 */

const KIND = { town: 'Town', district: 'District', building: 'Building', room: 'Room' } as const

type Props = {
  path: Path
  go: (path: Path) => void
  /** Open the same place in the dashboard style. */
  dashboardHref?: (place: Place) => string | null
  /** Drive frames by a timer instead of animation frames (the bench, in a hidden pane where animation frames stop). */
  timerLoop?: boolean
  /** What the places are and hold: the plug-in. The frame assumes nothing about it. */
  content: CityContent
  /** Where the Owner's positions are kept (display only). */
  arrangements?: ArrangementStore
}

const at = (r: Rect): CSSProperties => ({ left: r.x, top: r.y, width: r.w, height: r.h })

/** The notch and the home bar, read from CSS (env(safe-area-inset-*)) through a probe. */
function readInsets(probe: HTMLElement | null): Insets {
  if (!probe) return NO_INSETS
  const s = getComputedStyle(probe)
  const n = (v: string) => Number.parseFloat(v) || 0
  return { top: n(s.paddingTop), right: n(s.paddingRight), bottom: n(s.paddingBottom), left: n(s.paddingLeft) }
}

const LIGHT_KEY = 'bifrost.city.gen2.light'
const LIGHT_WORD: Record<LightChoice, string> = { auto: 'Auto', day: 'Day', dusk: 'Dusk', night: 'Night' }
const LIGHT_ICON = { day: '☀', dusk: '◐', night: '☾' } as const
const readLight = (): LightChoice => {
  try { const v = window.localStorage.getItem(LIGHT_KEY); return v && v in NEXT ? v as LightChoice : 'auto' } catch { return 'auto' }
}

const keyOf = (path: Path) => path.length === 0 ? 'town' : path.join('/')

export default function CityGen2({ path: asked, go, dashboardHref = p => p.dashboard, timerLoop = false, arrangements, content }: Props) {
  const store = useMemo(() => arrangements ?? browserArrangements(), [arrangements])
  const tree = content.tree
  const path = useMemo(() => resolve(tree, asked), [tree, asked])
  const steps = trail(tree, path)
  const place = here(tree, path)
  const parent = steps.length > 1 ? steps[steps.length - 2] : null
  const [selected, setSelected] = useState<Place | null>(null)
  const [open, setOpen] = useState(false)
  const [arranging, setArranging] = useState(false)
  // The drawer (phone, tablet upright): closed to its handle until opened or a room is picked.
  const [drawerOpen, setDrawerOpen] = useState(false)
  // A card opened large in the middle (by its panel id), and the search box.
  const [focused, setFocused] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [searching, setSearching] = useState(false)
  const searchBox = useRef<HTMLInputElement>(null)
  useEffect(() => { if (selected) setDrawerOpen(true) }, [selected])
  // Day, dusk or night: by the viewer's clock unless pinned; the clock is read every minute.
  const [lightChoice, setLightChoice] = useState<LightChoice>(readLight)
  const [now, setNow] = useState(() => new Date())
  useEffect(() => { const t = window.setInterval(() => setNow(new Date()), 60_000); return () => window.clearInterval(t) }, [])
  const light = lightOf(lightChoice, now)
  const stepLight = () => {
    const next = NEXT[lightChoice]
    setLightChoice(next)
    try { window.localStorage.setItem(LIGHT_KEY, next) } catch { /* kept for this page only */ }
  }
  // The Owner's positions for this place and its parent; bumped on every move so the page redraws.
  const [saved, setSaved] = useState<{ here: Arrangement; parent: Arrangement }>(() => ({ here: store.get(keyOf(path)), parent: store.get(keyOf(path.slice(0, -1))) }))
  const placed = useMemo(() => arrange(place.children, saved.here), [place, saved.here])
  const placedRef = useRef(placed)
  placedRef.current = placed
  const root = useRef<HTMLDivElement>(null)
  const host = useRef<HTMLDivElement>(null)
  const probe = useRef<HTMLDivElement>(null)
  const scene = useRef<PlaceScene | null>(null)
  const [size, setSize] = useState({ w: 1280, h: 800, safe: NO_INSETS })
  const f = useMemo(() => frameOf(size.w, size.h, size.safe), [size])
  const last = useRef<Path>(path)
  const goRef = useRef(go)
  goRef.current = go
  const pathRef = useRef(path)
  pathRef.current = path

  // Follow the box's size (window resize, rotation, a split view), not only the window's.
  useEffect(() => {
    const el = root.current
    if (!el) return
    const on = () => setSize({ w: el.clientWidth, h: el.clientHeight, safe: readInsets(probe.current) })
    on()
    const ro = new ResizeObserver(on)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  useEffect(() => { scene.current?.setLayout(f) }, [f])
  useEffect(() => { scene.current?.setLight(light) }, [light])

  useEffect(() => {
    if (!host.current) return
    const s = new PlaceScene({
      enter: child => { setSelected(null); goRef.current([...pathRef.current, child.id]) },
      select: child => setSelected(child),
      moved: (id, to) => {
        const next = move(placedRef.current, id, to)
        if (!next) return
        const a = toArrangement(next)
        store.set(keyOf(pathRef.current), a)
        setSaved(s => ({ ...s, here: a }))
      },
    })
    s.setLayout(f)
    s.setLight(light)
    scene.current = s
    const game = new Phaser.Game({
      type: Phaser.AUTO, parent: host.current, transparent: false, antialias: true,
      scale: { mode: Phaser.Scale.RESIZE, width: '100%', height: '100%' },
      scene: s, banner: false, fps: { forceSetTimeOut: timerLoop, target: 60 },
      input: { touch: { capture: true } },
      // Mipmaps: the town drawn small on a phone scales its sprites far down; without them the window rows shimmer.
      render: { mipmapFilter: 'LINEAR_MIPMAP_LINEAR' },
    })
    return () => { game.destroy(true); scene.current = null }
    // One game for the life of the page: the loop kind is read once; the layout follows through setLayout.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    const prev = last.current
    last.current = path
    const from = path.length > prev.length && prev.every((p, i) => path[i] === p) ? 'down'
      : path.length < prev.length && path.every((p, i) => prev[i] === p) ? 'up' : 'jump'
    setSelected(null)
    setOpen(false)
    setArranging(false)
    setDrawerOpen(false)
    setFocused(null)
    setSearching(false)
    setQuery('')
    const fresh = { here: store.get(keyOf(path)), parent: store.get(keyOf(path.slice(0, -1))) }
    setSaved(fresh)
    scene.current?.show(shown({ from, selected: null, arranging: false, here: fresh.here, parentA: fresh.parent }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path.join('/')])

  // Same page, something changed on it: the pick, arranging, a move.
  useEffect(() => {
    scene.current?.show(shown({ from: 'jump', selected: selected?.id ?? null, arranging, here: saved.here, parentA: saved.parent }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected?.id, arranging, saved])

  function shown(o: { from: Shown['from']; selected: string | null; arranging: boolean; here: Arrangement; parentA: Arrangement }): Shown {
    return {
      place, placed: arrange(place.children, o.here), parent, parentPlaced: parent ? arrange(parent.children, o.parentA) : [],
      inside: path.at(-1) ?? null, selected: o.selected, from: o.from, arranging: o.arranging,
    }
  }

  const resetArrangement = () => {
    store.set(keyOf(path), null)
    setSaved(s => ({ ...s, here: {} }))
  }

  // Esc: close the card, else go up one level.
  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      const typing = e.target instanceof HTMLInputElement
      if (e.key === '/' && !typing) { e.preventDefault(); setSearching(true); setTimeout(() => searchBox.current?.focus(), 0); return }
      if (e.key !== 'Escape') return
      if (searching) { setSearching(false); setQuery(''); searchBox.current?.blur(); return }
      if (focused) { setFocused(null); return }
      if (arranging) setArranging(false)
      else if (selected) setSelected(null)
      else if (pathRef.current.length > 0) goRef.current(pathRef.current.slice(0, -1))
    }
    window.addEventListener('keydown', on)
    return () => window.removeEventListener('keydown', on)
  }, [selected, arranging, focused, searching])

  const district = path[0] ?? null
  // A row in a table does what tapping its block does: a room opens its card, anything else is entered.
  const openChild = (c: Place) => c.kind === 'room' ? setSelected(c) : go([...path, c.id])
  const dash = dashboardHref(place)
  const panels: Panel[] = [...(selected ? [content.picked(selected)] : []), ...content.panels(place, openChild)]
  const focusedPanel = panels.find(p => p.id === focused) ?? null
  // Search: every place by its name or its question, with where it is.
  const index = useMemo(() => {
    const out: { path: Path; place: Place; where: string }[] = []
    const walk = (p: Place, at: Path, names: string[]) => p.children.forEach(c => {
      out.push({ path: [...at, c.id], place: c, where: names.join(' › ') })
      walk(c, [...at, c.id], [...names, c.label])
    })
    walk(tree, [], [tree.label])
    return out
  }, [tree])
  const q = query.trim().toLowerCase()
  const hits = q ? index.filter(h => h.place.label.toLowerCase().includes(q) || h.place.question.toLowerCase().includes(q)).slice(0, 8) : []
  const goHit = (h: { path: Path; place: Place }) => {
    setSearching(false); setQuery('')
    // A room opens in its building with its card up; anything else is entered.
    if (h.place.kind === 'room') { go(h.path.slice(0, -1)); setTimeout(() => setSelected(h.place), 0) } else go(h.path)
  }
  const tools = arranging ? (
    <>
      <button className="g2-btn primary" onClick={() => setArranging(false)}>Done</button>
      <button className="g2-btn" onClick={resetArrangement} disabled={Object.keys(saved.here).length === 0}>Reset</button>
    </>
  ) : (
    <>
      {place.children.length > 0 && <button className="g2-btn" onClick={() => { setSelected(null); setArranging(true) }}>Arrange</button>}
      <button className="g2-btn" onClick={stepLight} aria-label={`Light: ${LIGHT_WORD[lightChoice]}`}>
        {LIGHT_ICON[light]} {lightChoice === 'auto' ? `Auto · ${LIGHT_WORD[light]}` : LIGHT_WORD[lightChoice]}
      </button>
      {dash && <a className="g2-btn g2-swap" href={dash}>Dashboard ↗</a>}
    </>
  )
  const searchResults = (searching || q) && (
    <ul className="g2-hits" role="listbox" aria-label="Places found">
      {q && hits.length === 0 && <li className="g2-unknown">No place called “{query}”</li>}
      {hits.map(h => (
        <li key={h.path.join('/')}><button onMouseDown={e => e.preventDefault()} onClick={() => goHit(h)}>
          <b>{h.place.label}</b><span>{KIND[h.place.kind]} · {h.where}</span></button></li>
      ))}
    </ul>
  )
  const card = (p: Panel, docked: boolean) => (
    <section key={p.id} className={`g2-panelbox${p.id.startsWith('picked-') ? ' picked' : ''}${docked ? ' docked' : ''}`} aria-label={p.title}>
      <header>
        <h3>{p.title}</h3>
        {p.sample && <span className="g2-chip">sample</span>}
        <span className="g2-grow" />
        <button className="g2-icon" onClick={() => setFocused(p.id)} aria-label={`Open ${p.title} large`}>⤢</button>
        {p.id.startsWith('picked-') && <button className="g2-icon" onClick={() => setSelected(null)} aria-label="Close">×</button>}
      </header>
      <div className="g2-panelbody">{p.body}</div>
    </section>
  )
  const trailLinks = steps.map((s, i) => (
    <span key={s.id}>
      {i > 0 && <span className="g2-sep">›</span>}
      {i === steps.length - 1 ? <b>{s.label}</b> : <a href="#" onClick={e => { e.preventDefault(); go(path.slice(0, i)) }}>{s.label}</a>}
    </span>
  ))

  return (
    <div className="g2" ref={root} data-device={f.device} data-landscape={f.landscape || undefined} data-light={light}
      style={{ ...CORNERS[light], background: CORNERS[light]['--g2-sky'], color: CORNERS[light]['--g2-ink'] } as CSSProperties}>
      <div className="g2-probe" ref={probe} aria-hidden />
      <div className="g2-canvas" ref={host} />

      {/* Summary. On a phone one line; tap it to read the question. */}
      <section className={`g2-corner g2-tl${f.summary.compact ? ' compact' : ''}${open ? ' open' : ''}`} aria-label="Summary"
        style={{ left: f.summary.x, top: f.summary.y, width: f.summary.w, minHeight: f.summary.h }}
        onClick={f.summary.compact ? () => setOpen(o => !o) : undefined}>
        <div className="g2-kicker">{KIND[place.kind]}<span className="g2-chip">greybox</span>{f.summary.compact && <span className="g2-more">{open ? '▴' : '▾'}</span>}</div>
        <h1>{place.label}</h1>
        {(!f.summary.compact || open) && <p>{place.question}</p>}
        {!f.trail && steps.length > 1 && <nav className="g2-inline-trail" aria-label="Trail" onClick={e => e.stopPropagation()}>{trailLinks}</nav>}
        <dl>
          <div><dt>Inside</dt><dd>{place.children.length}</dd></div>
          <div><dt>Readings</dt><dd className="g2-unknown">not wired</dd></div>
        </dl>
        {/* On a phone the tools live here (the top bar has no room); elsewhere they sit beside the search. */}
        {f.search.mode === 'icon' && (!f.summary.compact || open || arranging) && <div className="g2-actions" onClick={e => e.stopPropagation()}>{tools}</div>}
        {arranging && f.search.mode === 'icon' && <p className="g2-hint">Drag a block to move it. Where things stand is yours; how big they are is not.</p>}
      </section>

      {/* The minimap: where you came from, shrunk. Tapping it goes back up. */}
      {parent && (
        <button className="g2-corner g2-tr" style={at(f.mini)} onClick={() => go(path.slice(0, -1))} aria-label={`Back to ${parent.label}`}>
          <span style={{ height: f.miniTitle, lineHeight: `${f.miniTitle}px` }}>↑ {parent.label}</span>
        </button>
      )}

      {f.trail && <nav className="g2-corner g2-bl" aria-label="Trail" style={at(f.trail)}>{trailLinks}</nav>}

      <aside className={`g2-corner g2-br${f.alerts.badge ? ' badge' : ''}`} aria-label="Alerts: not wired" style={at(f.alerts)}>
        {f.alerts.badge ? <span title="Alerts: not wired">!</span> : <>Alerts <span className="g2-unknown">not wired</span></>}
      </aside>

      <nav className="g2-menu" aria-label="Districts" style={at(f.menu)}>
        <button className={district == null ? 'on' : ''} onClick={() => go([])}>City</button>
        {tree.children.map(d => (
          <button key={d.id} className={district === d.id ? 'on' : ''} onClick={() => go([d.id])}>{d.label}</button>
        ))}
      </nav>

      {/* Search and tools: a bar at the top centre; on a phone a round button that opens the box over the top. */}
      {f.search.mode === 'bar' ? (
        <div className="g2-corner g2-search" style={at(f.search)}>
          {arranging
            ? <span className="g2-hint-inline">Drag a block to move it. Where things stand is yours; how big they are is not.</span>
            : <input ref={searchBox} value={query} onChange={e => setQuery(e.target.value)} onFocus={() => setSearching(true)} onBlur={() => setSearching(false)}
                onKeyDown={e => { if (e.key === 'Enter' && hits[0]) goHit(hits[0]) }} placeholder="Search places   /" aria-label="Search places" />}
          <div className="g2-tools">{tools}</div>
          {searchResults}
        </div>
      ) : (
        <>
          <button className="g2-corner g2-searchicon" style={at(f.search)} onClick={() => { setSearching(true); setTimeout(() => searchBox.current?.focus(), 0) }} aria-label="Search places">⌕</button>
          {searching && (
            <div className="g2-corner g2-search phone" style={{ left: f.summary.x, top: f.summary.y, width: f.mini.x + f.mini.w - f.summary.x, height: 48 }}>
              <input ref={searchBox} value={query} onChange={e => setQuery(e.target.value)} onBlur={() => setTimeout(() => setSearching(false), 150)}
                onKeyDown={e => { if (e.key === 'Enter' && hits[0]) goHit(hits[0]) }} placeholder="Search places" aria-label="Search places" />
              <button className="g2-icon" onClick={() => { setSearching(false); setQuery('') }} aria-label="Close search">×</button>
              {searchResults}
            </div>
          )}
        </>
      )}

      {/* The panels: small cards in the two bottom corners the diamond leaves empty (charts left, tables and a picked room right). */}
      {f.docks && (['left', 'right'] as const).map(side => {
        const mine = panels.filter(p => p.dock === side)
        return mine.length > 0 && (
          <aside key={side} className={`g2-dock ${side}`} aria-label={side === 'left' ? 'Charts' : 'Tables'} style={at(f.docks![side])}>
            {mine.map(p => card(p, true))}
          </aside>
        )
      })}

      {/* The drawer (phone, tablet upright): the same cards, pulled up from the bottom. */}
      {!f.docks && drawerOpen && (
        <aside className="g2-rail drawer" aria-label="Tables and charts"
          style={{ left: f.rail.x, bottom: size.h - (f.rail.y + f.rail.h), width: f.rail.w, height: f.rail.h }}>
          <button className="g2-grip-btn" onClick={() => { setDrawerOpen(false); setSelected(null) }} aria-label="Close tables and charts"><span /></button>
          {panels.map(p => card(p, false))}
        </aside>
      )}

      {/* A card opened large: in the middle, over the dimmed stage; the backdrop or Esc closes it. */}
      {focusedPanel && (
        <>
          <div className="g2-backdrop" onClick={() => setFocused(null)} />
          <section className="g2-focus" aria-label={focusedPanel.title} style={at(f.focus)}>
            <header>
              <h3>{focusedPanel.title}</h3>
              {focusedPanel.sample && <span className="g2-chip">sample</span>}
              <span className="g2-grow" />
              <button className="g2-icon" onClick={() => setFocused(null)} aria-label="Close">×</button>
            </header>
            <div className="g2-focusbody">{focusedPanel.body}</div>
          </section>
        </>
      )}
      {f.peek && !drawerOpen && (
        <button className="g2-corner g2-peek" style={at(f.peek)} onClick={() => setDrawerOpen(true)}>
          Tables and charts <span aria-hidden>▴</span>
        </button>
      )}
    </div>
  )
}
