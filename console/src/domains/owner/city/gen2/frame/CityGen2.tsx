import * as Phaser from 'phaser'
import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { browserArrangements, type ArrangementStore } from './arrangementStore'
import { KitRoot, Menu, type PanelSize } from './parts'
import { assign, decodePlan, encodePlan, mergePlot, moveTo, planFor, plotsOf, splitPlot, streetsOf, type Arrangement, type Item, type Plan } from './grid'
import { CORNERS, lightOf, NEXT, type LightChoice } from './light'
import type { CityContent, Panel } from './content'
import { PlaceScene, type ArrangeTool, type Shown } from './placeScene'
import { functionAt, here, isWired, meterText, resolve, trail, type Path, type Place } from './places'
import { rulesBroken } from './rules'
import { linksAt } from './links'
import { frameOf, NO_INSETS, swapRects, type Insets, type Rect } from './viewport'
import './cityGen2.css'

/**
 * The second generation's frame, greybox (the Owner, 2026-11-19): the game
 * is laid out like the Console, so the two looks can swap.
 *
 *   Menu      → the bar along the bottom: the Console's functions (one way in)
 *   Levels    → town, district, building: the framework's own control under the minimap (the other
 *               way in, by domain; both kept, the Owner 2026-11-19)
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

const KIND = { town: 'City', district: 'District', building: 'Building', room: 'Room' } as const

type Props = {
  path: Path
  go: (path: Path) => void
  /**
   * The same place in the Console (the Owner, 2026-11-19: the city and the Console are two looks of one
   * system, side by side; neither holds the other). The switch City | Console sits top right; the link
   * carries the place and the picked room, so the Console opens at the same spot.
   */
  consoleHref?: (place: Place, path: Path, picked: Place | null) => string | null
  /** A room to have picked when the page opens at this place (coming back from the Console). */
  picked?: string
  /** Drive frames by a timer instead of animation frames (the bench, in a hidden pane where animation frames stop). */
  timerLoop?: boolean
  /** What the places are and hold: the plug-in. The frame assumes nothing about it. */
  content: CityContent
  /** Where the Owner's positions are kept (display only). */
  arrangements?: ArrangementStore
  /** Said at the top of the summary, for a page that cannot read the system (rule: nothing made up, so it says so). */
  notice?: string
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

/**
 * The panels' rules (the Owner, 2026-11-19: a framework must say how its windows come, go and come
 * back). A panel is open (a card in its corner), folded (its title as a tab in the same corner) or in
 * the middle. A card folds with –, goes to the middle with ⤢; a tab opens it again where it was; ×
 * in the middle sends it back to its corner. Nothing is ever lost: only a picked room's card closes,
 * and picking the room brings it back. Panels hides both corners at once (key H) and shows them again.
 * Folds and the hide are kept in the browser, across places.
 */
const PANELS_KEY = 'bifrost.city.gen2.panels.v1'
type PanelPrefs = { folded: Record<string, boolean>; hidden: boolean }
function readPanels(): PanelPrefs {
  try {
    const v = JSON.parse(window.localStorage.getItem(PANELS_KEY) ?? 'null')
    if (v && typeof v === 'object' && typeof v.hidden === 'boolean' && v.folded && typeof v.folded === 'object') return v as PanelPrefs
  } catch { /* fall through */ }
  return { folded: {}, hidden: false }
}
const LIGHT_WORD: Record<LightChoice, string> = { auto: 'Auto', day: 'Day', dusk: 'Dusk', night: 'Night' }
const LIGHT_ICON = { day: '☀', dusk: '◐', night: '☾' } as const
const readLight = (): LightChoice => {
  try { const v = window.localStorage.getItem(LIGHT_KEY); return v && v in NEXT ? v as LightChoice : 'auto' } catch { return 'auto' }
}

/** A place's ground as the Owner left it: its plan (or one cut to fit its things), its plots and streets, and where each thing stands. */
function layoutOf(place: Place, a: Arrangement) {
  const children: readonly Item[] = place.children
  // The Owner's own arrangement first; else the domain's (Place.arrangement); else one cut to fit.
  const own = place.arrangement
  const plan: Plan = (a.plan && decodePlan(a.plan)) || (own && decodePlan(own.plan)) || planFor(children)
  const plots = plotsOf(plan)
  const { placed, homeless } = assign(children, plots, a.at ?? (a.plan ? {} : own?.at ?? {}))
  return { plan, plots, streets: streetsOf(plan), placed, homeless }
}

const TOOL_HINT: Record<ArrangeTool, string> = {
  move: 'Drag a building onto a plot. Onto a taken plot, the two swap.',
  split: 'Tap a plot to cut it in two. The streets follow.',
  merge: 'Tap a plot to join it with its other half.',
}

const keyOf = (path: Path) => path.length === 0 ? 'town' : path.join('/')

/**
 * The frame's door: content is checked against the frame's rules (rules.ts) before anything is drawn.
 * Content that breaks one is refused with what it broke (the Owner, 2026-11-19: a domain that uses
 * this look keeps its rules; it cannot change them).
 */
export default function CityGen2(props: Props) {
  const broken = useMemo(() => rulesBroken(props.content), [props.content])
  if (broken.length > 0) return (
    <div className="g2 g2-refused" role="alert" style={{ ...CORNERS.day, background: CORNERS.day['--g2-sky'], color: CORNERS.day['--g2-ink'] } as CSSProperties}>
      <section className="g2-corner">
        <h1>This content breaks the frame’s rules</h1>
        <p>The frame draws only content that keeps its rules. Fix the content; the frame does not bend.</p>
        <ul>{broken.map(b => <li key={b}><code>{b}</code></li>)}</ul>
      </section>
    </div>
  )
  return <CityFrame {...props} />
}

function CityFrame({ path: asked, go, consoleHref = p => p.dashboard, picked, timerLoop = false, arrangements, content, notice }: Props) {
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
  // The panel that has the middle (by its id; null: the map has it), and the search box.
  const [focused, setFocused] = useState<string | null>(null)
  // Zoomed closer than the whole page: the minimap shows this level and the part on screen.
  const [zoomed, setZoomed] = useState(false)
  const [panelPrefs, setPanelPrefs] = useState<PanelPrefs>(readPanels)
  const keepPanels = (next: PanelPrefs) => {
    setPanelPrefs(next)
    try { window.localStorage.setItem(PANELS_KEY, JSON.stringify(next)) } catch { /* kept for this page only */ }
  }
  const fold = (id: string, on: boolean) => keepPanels({ ...panelPrefs, folded: { ...panelPrefs.folded, [id]: on } })
  const toggleHidden = () => keepPanels({ ...panelPrefs, hidden: !panelPrefs.hidden })
  const toggleHiddenRef = useRef(toggleHidden)
  toggleHiddenRef.current = toggleHidden
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
  const [tool, setTool] = useState<ArrangeTool>('move')
  // The menu: the content's functions, a lens over the map (the Owner, 2026-11-19: by function beside
  // the levels by domain). Picking one lights up what serves it, here and on every level; picking it again clears it.
  const functions = content.functions ?? []
  const [lens, setLens] = useState<string | null>(null)
  const lensLabel = functions.find(fn => fn.id === lens)?.label
  const lensHere = lens ? place.children.filter(c => functionAt(tree, [...path, c.id]) === lens).length : 0
  const lay = useMemo(() => layoutOf(place, saved.here), [place, saved.here])
  const layRef = useRef(lay)
  layRef.current = lay
  const root = useRef<HTMLDivElement>(null)
  // Dialogs and menus open in the frame's root, so they take its colours and cover the whole frame.
  const [rootEl, setRootEl] = useState<HTMLElement | null>(null)
  useEffect(() => { setRootEl(root.current) }, [])
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
      moved: (id, plotId) => {
        const L = layRef.current
        const at = moveTo(L.placed, L.plots, id, plotId)
        if (at) keep({ plan: encodePlan(L.plan), at })
      },
      plot: (op, plotId) => {
        const L = layRef.current
        const next = op === 'split' ? splitPlot(L.plan, plotId) : mergePlot(L.plan, plotId)
        // A cut or a join changes the plots; things keep their plot where it still exists and fits.
        if (next !== null) keep({ plan: encodePlan(next), at: Object.fromEntries(L.placed.map(p => [p.id, p.plot])) })
      },
      zoomed: on => setZoomed(on),
      up: () => { if (pathRef.current.length > 0) { setSelected(null); goRef.current(pathRef.current.slice(0, -1)) } },
      // A neighbour's icon on the edge (a district it talks to): straight there.
      go: to => { setSelected(null); goRef.current(to) },
    })
    s.setLayout(f)
    s.setLight(light)
    scene.current = s
    const game = new Phaser.Game({
      // Transparent: when the map draws in its medium card, the page's own sky shows round it.
      type: Phaser.AUTO, parent: host.current, transparent: true, antialias: true,
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
    // Back from the Console with a room picked there: picked here too (the address carries it).
    const pick = place.children.find(x => x.id === picked && x.kind === 'room') ?? null
    setSelected(pick)
    setOpen(false)
    setArranging(false)
    setDrawerOpen(false)
    setFocused(null)
    setSearching(false)
    setQuery('')
    const fresh = { here: store.get(keyOf(path)), parent: store.get(keyOf(path.slice(0, -1))) }
    setSaved(fresh)
    scene.current?.show(shown({ from, selected: pick?.id ?? null, arranging: false, here: fresh.here, parentA: fresh.parent }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path.join('/')])

  // Same page, something changed on it: the pick, arranging, a move.
  useEffect(() => {
    scene.current?.show(shown({ from: 'jump', selected: selected?.id ?? null, arranging, here: saved.here, parentA: saved.parent }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected?.id, arranging, saved, tool, lens, place, parent])

  function shown(o: { from: Shown['from']; selected: string | null; arranging: boolean; here: Arrangement; parentA: Arrangement }): Shown {
    const h = layoutOf(place, o.here)
    const p = parent ? layoutOf(parent, o.parentA) : null
    // Any place's ground, for the miniatures (a district seen from the town, a building's wings).
    const layoutAt = (at: Path) => {
      const t = trail(tree, at)
      if (t.length !== at.length + 1) return null
      const l = layoutOf(t[t.length - 1], store.get(keyOf(at)))
      return { placed: l.placed, plots: l.plots, streets: l.streets }
    }
    return {
      tree, path, layoutAt,
      place, placed: h.placed, plots: h.plots, streets: h.streets,
      parent, parentPlaced: p?.placed ?? [], parentPlots: p?.plots ?? [], parentStreets: p?.streets ?? [],
      inside: path.at(-1) ?? null, selected: o.selected, from: o.from, arranging: o.arranging, tool, lens,
      links: linksAt(tree, path, content.links ?? []),
      allLinks: content.links ?? [],
    }
  }

  /** Keep the Owner's plan and plots for this place (display only). */
  function keep(a: Arrangement) {
    store.set(keyOf(pathRef.current), a)
    setSaved(s => ({ ...s, here: a }))
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
      if ((e.key === 'h' || e.key === 'H') && !typing && !e.metaKey && !e.ctrlKey && !e.altKey) { toggleHiddenRef.current(); return }
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

  // The levels (the Owner, 2026-11-19: three — town, district, building — moved between by a control
  // of their own). Going up is always there; going down goes back to the district and building
  // last visited, or is not there yet.
  const [visited, setVisited] = useState<Record<string, string>>({})
  useEffect(() => {
    if (path.length === 0) return
    setVisited(l => ({ ...l, '': path[0], ...(path.length > 1 ? { [path[0]]: path[1] } : {}) }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path.join('/')])
  const depth = Math.min(path.length, 2)
  const levelDistrict = tree.children.find(c => c.id === (path[0] ?? visited['']))
  const levelBuilding = levelDistrict?.children.find(c => c.id === (path[1] ?? visited[levelDistrict.id]))
  const levelRows: { name: string; label: string | null; to: Path | null }[] = [
    { name: 'City', label: tree.label, to: [] },
    { name: 'District', label: levelDistrict?.label ?? null, to: levelDistrict ? [levelDistrict.id] : null },
    { name: 'Building', label: levelBuilding?.label ?? null, to: levelDistrict && levelBuilding ? [levelDistrict.id, levelBuilding.id] : null },
  ]
  // A row in a table does what tapping its block does: a room opens its card, anything else is entered.
  const openChild = (c: Place) => c.kind === 'room' ? setSelected(c) : go([...path, c.id])
  const dash = consoleHref(place, path, selected)
  const panels: Panel[] = [...(selected ? [content.picked(selected)] : []), ...content.panels(place, openChild)]
  const focusedPanel = panels.find(p => p.id === focused) ?? null
  // When a panel has the middle, the map supports it from the panel's corner (or the top, on a phone).
  const swap = focusedPanel ? swapRects(f, focusedPanel.dock) : null
  // The map follows the middle: the whole screen, or its medium card while a panel has the middle.
  const mapRect = swap ? swap.map : null
  useEffect(() => { scene.current?.setStage(mapRect) }, [mapRect?.x, mapRect?.y, mapRect?.w, mapRect?.h]) // eslint-disable-line react-hooks/exhaustive-deps
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
      <div className="k-seg" role="group" aria-label="Arrange">
        {(['move', 'split', 'merge'] as const).map(t => (
          <button key={t} className={tool === t ? 'on' : ''} aria-pressed={tool === t} onClick={() => setTool(t)}>{{ move: 'Move', split: 'Cut', merge: 'Join' }[t]}</button>
        ))}
      </div>
      <button className="g2-btn" onClick={resetArrangement} disabled={!saved.here.plan && !saved.here.at}>Reset</button>
      <button className="g2-btn primary" onClick={() => { setArranging(false); setTool('move') }}>Done</button>
    </>
  ) : (
    <>
      {place.children.length > 0 && <button className="g2-btn" onClick={() => { setSelected(null); setArranging(true) }}>Arrange</button>}
      <button className="g2-btn" onClick={stepLight} aria-label={`Light: ${LIGHT_WORD[lightChoice]}`}>
        {LIGHT_ICON[light]} {lightChoice === 'auto' ? `Auto · ${LIGHT_WORD[light]}` : LIGHT_WORD[lightChoice]}
      </button>
      {f.docks && <button className={`g2-icon g2-panels${panelPrefs.hidden ? '' : ' on'}`} onClick={toggleHidden} aria-pressed={!panelPrefs.hidden}
        aria-label={panelPrefs.hidden ? 'Show the panels' : 'Hide the panels'} title={panelPrefs.hidden ? 'Show the panels (H)' : 'Hide the panels (H)'}>▤</button>}
      {dash && <div className="k-seg g2-looks" role="group" aria-label="Look">
        <button className="on" aria-pressed>City</button>
        <a href={dash} title="The same place in the Console">Console</a>
      </div>}
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
  const bodyOf = (p: Panel, size: PanelSize) => typeof p.body === 'function' ? p.body({ size }) : p.body
  const card = (p: Panel, docked: boolean) => (
    <section key={p.id} className={`g2-panelbox${p.id.startsWith('picked-') ? ' picked' : ''}${docked ? ' docked' : ''}`} aria-label={p.title}>
      <header>
        <h3>{p.title}</h3>
        <span className="g2-grow" />
        {p.menu && p.menu.length > 0 && <Menu items={p.menu} label={`More for ${p.title}`} />}
        {!p.id.startsWith('picked-') && <button className="g2-icon" onClick={() => fold(p.id, true)} aria-label={`Fold ${p.title}`} title="Fold to a tab">–</button>}
        <button className="g2-icon" onClick={() => { setDrawerOpen(false); setFocused(p.id) }} aria-label={`Put ${p.title} in the middle`} title="Put in the middle">⤢</button>
        {p.id.startsWith('picked-') && <button className="g2-icon" onClick={() => setSelected(null)} aria-label="Close">×</button>}
      </header>
      <div className="g2-panelbody">{bodyOf(p, docked ? 'card' : 'drawer')}</div>
    </section>
  )

  return (
    <div className="g2" ref={root} data-device={f.device} data-landscape={f.landscape || undefined} data-light={light}
      style={{ ...CORNERS[light], background: CORNERS[light]['--g2-sky'], color: CORNERS[light]['--g2-ink'] } as CSSProperties}>
      <KitRoot value={rootEl}>
      <div className="g2-probe" ref={probe} aria-hidden />
      <div className="g2-canvas" ref={host} />

      {/* Summary. On a phone one line; tap it to read the question. */}
      <section className={`g2-corner g2-tl${f.summary.compact ? ' compact' : ''}${open ? ' open' : ''}`} aria-label="Summary"
        style={{ left: f.summary.x, top: f.summary.y, width: f.summary.w, minHeight: f.summary.h }}
        onClick={f.summary.compact ? () => setOpen(o => !o) : undefined}>
        <div className="g2-kicker">{KIND[place.kind]}{!isWired(place) && <span className="g2-chip">not wired</span>}{f.summary.compact && <span className="g2-more">{open ? '▴' : '▾'}</span>}</div>
        {notice && <p className="g2-hint g2-warn" role="status">{notice}</p>}
        <h1>{place.label}</h1>
        {(!f.summary.compact || open) && <p>{place.question}</p>}
        <dl>
          <div><dt>Inside</dt><dd>{place.children.length}</dd></div>
          <div><dt>Readings</dt><dd className={place.state === 'online' || (!place.state && isWired(place)) ? '' : 'g2-unknown'}>{place.meter ? meterText(place.meter) : place.state ?? (isWired(place) ? 'read inside' : 'not wired')}</dd></div>
        </dl>
        {/* On a phone the tools live here (the top bar has no room); elsewhere they sit beside the search. */}
        {f.search.mode === 'icon' && (!f.summary.compact || open || arranging) && <div className="g2-actions" onClick={e => e.stopPropagation()}>{tools}</div>}
        {arranging && f.search.mode === 'icon' && <p className="g2-hint">{TOOL_HINT[tool]}</p>}
        {lensLabel && <p className="g2-hint">Lit: <b>{lensLabel}</b> · {lensHere === 0 ? 'nothing here serves it' : `${lensHere} here`}</p>}
        {lay.homeless.length > 0 && <p className="g2-hint g2-warn">{lay.homeless.length === 1 ? 'One building finds' : `${lay.homeless.length} buildings find`} no plot big enough. Join plots to make room.</p>}
      </section>

      {/* The minimap: where you came from, shrunk. Tapping it goes back up. */}
      {(parent || zoomed) && (
        zoomed
          ? <button className="g2-corner g2-tr" style={at(f.mini)} onClick={() => scene.current?.fitAll()} aria-label={`Back to the whole of ${place.label}`}>
              <span style={{ height: f.miniTitle, lineHeight: `${f.miniTitle}px` }}>◎ {place.label}</span>
            </button>
          : <button className="g2-corner g2-tr" style={at(f.mini)} onClick={() => go(path.slice(0, -1))} aria-label={`Back to ${parent!.label}`}>
              <span style={{ height: f.miniTitle, lineHeight: `${f.miniTitle}px` }}>↑ {parent!.label}</span>
            </button>
      )}


      <aside className={`g2-corner g2-br g2-ghost${f.alerts.badge ? ' badge' : ''}`} aria-label="Alerts: not wired" style={at(f.alerts)}>
        {f.alerts.badge ? <span title="Alerts: not wired">!</span> : <>Alerts <span className="g2-unknown">not wired</span></>}
      </aside>

      <nav className="g2-corner g2-levels" aria-label="Levels" style={at(f.levels)}>
        {levelRows.map((r, i) => (
          <button key={r.name} className={i === depth ? 'on' : ''} style={{ height: f.levels.row }} disabled={!r.to || i === depth}
            aria-current={i === depth ? 'location' : undefined} onClick={() => { if (r.to) go(r.to) }}
            title={r.to ? `${r.name}: ${r.label}` : `${r.name}: none visited yet`}>
            <span className="g2-lv-k">{r.name}</span><b>{r.label ?? '—'}</b>
          </button>
        ))}
      </nav>

      {/* Two ways in, both kept (the Owner, 2026-11-19): the menu by function along the bottom, the levels by domain under the minimap. */}
      <nav className="g2-menu" aria-label="Functions" style={at(f.menu)}>
        {functions.map(fn => (
          <button key={fn.id} className={lens === fn.id ? 'on' : ''} aria-pressed={lens === fn.id} onClick={() => setLens(l => l === fn.id ? null : fn.id)}
            title={lens === fn.id ? `Stop lighting up ${fn.label}` : `Light up what serves ${fn.label}`}>{fn.label}</button>
        ))}
      </nav>

      {/* Search and tools: a bar at the top centre; on a phone a round button that opens the box over the top. */}
      {f.search.mode === 'bar' ? (
        <div className="g2-corner g2-search" style={at(f.search)}>
          {arranging
            ? <span className="g2-hint-inline">{TOOL_HINT[tool]}</span>
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
      {f.docks && !panelPrefs.hidden && (['left', 'right'] as const).map(side => {
        // The dock the map moved into holds the map's card alone; the panel in the middle is not docked twice.
        if (swap && focusedPanel?.dock === side) return null
        const mine = panels.filter(p => p.dock === side && p.id !== focused)
        const isFolded = (p: Panel) => !p.id.startsWith('picked-') && !!panelPrefs.folded[p.id]
        const tabs = mine.filter(isFolded)
        return mine.length > 0 && (
          <aside key={side} className={`g2-dock ${side}`} aria-label={side === 'left' ? 'Charts' : 'Tables'} style={at(f.docks![side])}>
            {mine.filter(p => !isFolded(p)).map(p => card(p, true))}
            {tabs.length > 0 && (
              <div className="g2-tabs" role="group" aria-label="Folded panels">
                {tabs.map(p => <button key={p.id} className="g2-tab" onClick={() => fold(p.id, false)} title={`Open ${p.title}`}>{p.title} <span aria-hidden>▴</span></button>)}
              </div>
            )}
          </aside>
        )
      })}

      {/* The drawer (phone, tablet upright): the same cards, pulled up from the bottom. */}
      {!f.docks && drawerOpen && (
        <aside className="g2-rail drawer" aria-label="Tables and charts"
          style={{ left: f.rail.x, bottom: size.h - (f.rail.y + f.rail.h), width: f.rail.w, height: f.rail.h }}>
          <button className="g2-grip-btn" onClick={() => { setDrawerOpen(false); setSelected(null) }} aria-label="Close tables and charts"><span /></button>
          {panels.filter(p => p.id.startsWith('picked-') || !panelPrefs.folded[p.id]).map(p => card(p, false))}
          {panels.some(p => !p.id.startsWith('picked-') && panelPrefs.folded[p.id]) && (
            <div className="g2-tabs" role="group" aria-label="Folded panels">
              {panels.filter(p => !p.id.startsWith('picked-') && panelPrefs.folded[p.id]).map(p => (
                <button key={p.id} className="g2-tab" onClick={() => fold(p.id, false)} title={`Open ${p.title}`}>{p.title} <span aria-hidden>▴</span></button>
              ))}
            </div>
          )}
        </aside>
      )}

      {f.peek && !drawerOpen && !focused && (
        <button className="g2-corner g2-peek" style={at(f.peek)} onClick={() => setDrawerOpen(true)}>
          Tables and charts <span aria-hidden>▴</span>
        </button>
      )}

      {/* A panel in the middle, and the map as its medium card where the panel was; ⤢ on the map, or Esc, swaps back. */}
      {focusedPanel && swap && (
        <>
          <section className="g2-centre" aria-label={focusedPanel.title} style={at(swap.panel)}>
            <header>
              <h3>{focusedPanel.title}</h3>
              <span className="g2-grow" />
              {focusedPanel.menu && focusedPanel.menu.length > 0 && <Menu items={focusedPanel.menu} label={`More for ${focusedPanel.title}`} />}
              <button className="g2-icon" onClick={() => setFocused(null)} aria-label="Put the map back in the middle" title="Map to the middle">×</button>
            </header>
            <div className="g2-focusbody">{bodyOf(focusedPanel, 'middle')}</div>
          </section>
          <div className="g2-mapcard" style={at(swap.map)} aria-label={`Map: ${place.label}`}>
            <header>
              <h3>Map · {place.label}</h3>
              <span className="g2-grow" />
              <button className="g2-icon" onClick={() => setFocused(null)} aria-label="Put the map in the middle" title="Map to the middle">⤢</button>
            </header>
          </div>
        </>
      )}
      </KitRoot>
    </div>
  )
}
