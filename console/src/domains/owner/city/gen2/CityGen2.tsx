import * as Phaser from 'phaser'
import { useEffect, useMemo, useRef, useState } from 'react'
import { miniOf, PlaceScene } from './placeScene'
import { here, placeTree, resolve, trail, type Path, type Place } from './places'
import './cityGen2.css'

/**
 * The second generation's frame, greybox (the Owner, 2026-11-19): the game
 * is laid out like the Console, so the two looks can swap.
 *
 *   Menu      → the bar along the bottom: one button per district
 *   Page      → the place on screen; entering a building is opening a page
 *   Summary   → the card in the top-left corner
 *   Section   → the blocks inside a place
 *   Button    → a block you can click
 *   Popup     → the card on the right, for a room (a leaf)
 *   Back      → the minimap in the top-right corner (the place you came
 *               from, shrunk), or the trail in the bottom-left
 *
 * The bottom-right corner is kept for alerts. Every place has an address,
 * so Back in the browser works and a place can be sent as a link.
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

export default function CityGen2({ path: asked, go, dashboardHref = p => p.dashboard, timerLoop = false }: Props) {
  const tree = useMemo(() => placeTree(), [])
  const path = useMemo(() => resolve(tree, asked), [tree, asked])
  const steps = trail(tree, path)
  const place = here(tree, path)
  const parent = steps.length > 1 ? steps[steps.length - 2] : null
  const [selected, setSelected] = useState<Place | null>(null)
  const [width, setWidth] = useState(() => typeof window === 'undefined' ? 1200 : window.innerWidth)
  useEffect(() => {
    const on = () => setWidth(host.current?.clientWidth ?? window.innerWidth)
    on()
    window.addEventListener('resize', on)
    return () => window.removeEventListener('resize', on)
  }, [])
  const MINI = miniOf(width)
  const host = useRef<HTMLDivElement>(null)
  const scene = useRef<PlaceScene | null>(null)
  const last = useRef<Path>(path)
  const goRef = useRef(go)
  goRef.current = go
  const pathRef = useRef(path)
  pathRef.current = path

  useEffect(() => {
    if (!host.current) return
    const s = new PlaceScene({
      enter: child => { setSelected(null); goRef.current([...pathRef.current, child.id]) },
      select: child => setSelected(child),
    })
    scene.current = s
    const game = new Phaser.Game({
      type: Phaser.AUTO, parent: host.current, transparent: false, antialias: true,
      scale: { mode: Phaser.Scale.RESIZE, width: '100%', height: '100%' },
      scene: s, banner: false, fps: { forceSetTimeOut: timerLoop, target: 60 },
    })
    return () => { game.destroy(true); scene.current = null }
    // One game for the life of the page: the loop kind is read once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    const prev = last.current
    last.current = path
    const from = path.length > prev.length && prev.every((p, i) => path[i] === p) ? 'down'
      : path.length < prev.length && path.every((p, i) => prev[i] === p) ? 'up' : 'jump'
    if (from !== 'jump' || prev.join('/') !== path.join('/')) setSelected(null)
    scene.current?.show({ place, parent, inside: path.at(-1) ?? null, selected: selected?.id ?? null, from })
    // `selected` is shown by its own effect below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path.join('/')])

  useEffect(() => {
    scene.current?.show({ place, parent, inside: path.at(-1) ?? null, selected: selected?.id ?? null, from: 'jump' })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected?.id])

  const district = path[0] ?? null
  const dash = dashboardHref(place)

  return (
    <div className="g2">
      <div className="g2-canvas" ref={host} />

      {/* Top-left: the page's summary. */}
      <section className="g2-corner g2-tl" aria-label="Summary">
        <div className="g2-kicker">{KIND[place.kind]}<span className="g2-chip">greybox</span></div>
        <h1>{place.label}</h1>
        <p>{place.question}</p>
        <dl>
          <div><dt>Inside</dt><dd>{place.children.length}</dd></div>
          <div><dt>Readings</dt><dd className="g2-unknown">not wired</dd></div>
        </dl>
        {dash && <a className="g2-swap" href={dash}>Dashboard view ↗</a>}
      </section>

      {/* Top-right: where you came from, shrunk. Clicking it goes back up. */}
      {parent && (
        <button className="g2-corner g2-tr" style={{ width: MINI.w, height: MINI.h, top: MINI.margin, right: MINI.margin }}
          onClick={() => go(path.slice(0, -1))} aria-label={`Back to ${parent.label}`}>
          <span>↑ {parent.label}</span>
        </button>
      )}

      {/* Bottom-left: the trail. */}
      <nav className="g2-corner g2-bl" aria-label="Trail">
        {steps.map((s, i) => (
          <span key={s.id}>
            {i > 0 && <span className="g2-sep">›</span>}
            {i === steps.length - 1 ? <b>{s.label}</b> : <a href="#" onClick={e => { e.preventDefault(); go(path.slice(0, i)) }}>{s.label}</a>}
          </span>
        ))}
      </nav>

      {/* Bottom-right: kept for alerts. */}
      <aside className="g2-corner g2-br" aria-label="Alerts">Alerts <span className="g2-unknown">not wired</span></aside>

      {/* Bottom: the menu. */}
      <nav className="g2-menu" aria-label="Districts">
        <button className={district == null ? 'on' : ''} onClick={() => go([])}>City</button>
        {tree.children.map(d => (
          <button key={d.id} className={district === d.id ? 'on' : ''} onClick={() => go([d.id])}>{d.label}</button>
        ))}
      </nav>

      {/* Right: the card of a room you clicked (the popup of a page). */}
      {selected && (
        <aside className="g2-panel" aria-label={selected.label}>
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
