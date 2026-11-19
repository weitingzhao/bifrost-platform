import * as Phaser from 'phaser'
import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { PlaceScene } from './placeScene'
import { here, placeTree, resolve, trail, type Path, type Place } from './places'
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
}

const at = (r: Rect): CSSProperties => ({ left: r.x, top: r.y, width: r.w, height: r.h })

/** The notch and the home bar, read from CSS (env(safe-area-inset-*)) through a probe. */
function readInsets(probe: HTMLElement | null): Insets {
  if (!probe) return NO_INSETS
  const s = getComputedStyle(probe)
  const n = (v: string) => Number.parseFloat(v) || 0
  return { top: n(s.paddingTop), right: n(s.paddingRight), bottom: n(s.paddingBottom), left: n(s.paddingLeft) }
}

export default function CityGen2({ path: asked, go, dashboardHref = p => p.dashboard, timerLoop = false }: Props) {
  const tree = useMemo(() => placeTree(), [])
  const path = useMemo(() => resolve(tree, asked), [tree, asked])
  const steps = trail(tree, path)
  const place = here(tree, path)
  const parent = steps.length > 1 ? steps[steps.length - 2] : null
  const [selected, setSelected] = useState<Place | null>(null)
  const [open, setOpen] = useState(false)
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

  useEffect(() => {
    if (!host.current) return
    const s = new PlaceScene({
      enter: child => { setSelected(null); goRef.current([...pathRef.current, child.id]) },
      select: child => setSelected(child),
    })
    s.setLayout(f)
    scene.current = s
    const game = new Phaser.Game({
      type: Phaser.AUTO, parent: host.current, transparent: false, antialias: true,
      scale: { mode: Phaser.Scale.RESIZE, width: '100%', height: '100%' },
      scene: s, banner: false, fps: { forceSetTimeOut: timerLoop, target: 60 },
      input: { touch: { capture: true } },
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
    scene.current?.show({ place, parent, inside: path.at(-1) ?? null, selected: null, from })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path.join('/')])

  useEffect(() => {
    scene.current?.show({ place, parent, inside: path.at(-1) ?? null, selected: selected?.id ?? null, from: 'jump' })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected?.id])

  // Esc: close the card, else go up one level.
  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      if (selected) setSelected(null)
      else if (pathRef.current.length > 0) goRef.current(pathRef.current.slice(0, -1))
    }
    window.addEventListener('keydown', on)
    return () => window.removeEventListener('keydown', on)
  }, [selected])

  const district = path[0] ?? null
  const dash = dashboardHref(place)
  const trailLinks = steps.map((s, i) => (
    <span key={s.id}>
      {i > 0 && <span className="g2-sep">›</span>}
      {i === steps.length - 1 ? <b>{s.label}</b> : <a href="#" onClick={e => { e.preventDefault(); go(path.slice(0, i)) }}>{s.label}</a>}
    </span>
  ))

  return (
    <div className="g2" ref={root} data-device={f.device} data-landscape={f.landscape || undefined}>
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
        {dash && (!f.summary.compact || open) && <a className="g2-swap" href={dash} onClick={e => e.stopPropagation()}>Dashboard view ↗</a>}
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

      {selected && (
        <aside className={`g2-panel${f.card.sheet ? ' sheet' : ''}`} aria-label={selected.label} style={f.card.sheet
            // A bottom sheet grows up from the row above the menu; a side panel hangs from below the minimap.
            ? { left: f.card.x, bottom: size.h - (f.card.y + f.card.h), width: f.card.w, maxHeight: f.card.h }
            : { left: f.card.x, top: f.card.y, width: f.card.w, maxHeight: f.card.h }}>
          {f.card.sheet && <div className="g2-grip" aria-hidden />}
          <button className="g2-close" onClick={() => setSelected(null)} aria-label="Close">×</button>
          <div className="g2-kicker">Room<span className="g2-chip">greybox</span></div>
          <h2>{selected.label}</h2>
          <p>{selected.question}</p>
          <dl><div><dt>Readings</dt><dd className="g2-unknown">not wired</dd></div></dl>
        </aside>
      )}
    </div>
  )
}
