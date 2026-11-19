import { useCallback, useEffect, useMemo, useRef, useState, type ChangeEvent, type PointerEvent as RP } from 'react'
import { emptyDoc, expand, fromTiled, LAND_MAX, toTiled, type Doc, type Placed } from './builderDoc'
import { accepts, CATALOG, kindOf, showcaseLayers, spriteOf, spriteUrl, type CatalogKind, type SystemItem } from './catalog'
import type { Armed, BuilderScene, Tool } from './builderScene'
import type { Reading, Sky } from './cityReadings'
import type { Worker } from './cityWorkers'
import { DICTIONARY, DICTIONARY_VERSION } from './worldDictionary'
import { ZONE_NAMES, zoneTint } from './zones'
import { LAYOUT_FILE, layoutFileText, parseLayoutFile, type LayoutStore } from './layoutStore'

/**
 * The city builder's page: the land in the middle, the tools over it, the
 * catalogue and the tray beside it (under it on a phone). The Owner builds;
 * the document is his and is saved only when he says so. What exists in the
 * system comes in as `items`; nothing goes on the land by itself.
 */

type Tab = 'buildings' | 'unplaced' | 'selected'
type Armable = NonNullable<Armed>

// The city's own paper palette, the same in every Console theme.
const INK = '#2f2d3a', PAPER = '#fbfaf6', PAGE = '#f3efe6', MUTED = '#6b6458', HOT = '#ffd05a', LINE = '#d8d1c2'

const CSS = `
.cb{display:grid;grid-template-columns:minmax(0,1fr) 304px;gap:12px;height:100%;min-height:0;box-sizing:border-box;padding:12px;background:${PAGE};color:${INK};font:12px/1.35 'JetBrains Mono',ui-monospace,Menlo,monospace}
.cb *{box-sizing:border-box}
.cb-stage{position:relative;min-width:0;min-height:0;border:1.5px solid ${INK};background:${PAGE};overflow:hidden}
.cb-canvas{position:absolute;inset:0;touch-action:none}
.cb-bar{position:absolute;display:flex;gap:6px;align-items:center;flex-wrap:wrap}
.cb-tl{left:10px;top:10px;right:150px}.cb-tr{right:10px;top:10px}
.cb button{font:inherit;color:inherit;cursor:pointer}
.cb-btn{min-height:44px;min-width:44px;padding:0 12px;border:1.5px solid ${INK};background:${PAPER}}
.cb-btn[aria-pressed=true]{background:${INK};color:${PAPER}}
.cb-btn.go{background:${HOT}}.cb-btn:disabled{opacity:.45;cursor:default}
.cb-glance{display:inline-flex;align-items:center;min-height:32px;padding:0 10px;border:1.5px solid ${INK};color:${INK};text-decoration:none;font-weight:700}
.cb-sheet{position:absolute;left:10px;bottom:10px;width:min(340px,calc(100% - 20px));background:${PAPER};border:1.5px solid ${INK};padding:10px;z-index:4}
.cb-sheet h3{margin:0 0 6px;font-size:13px}.cb-sheet p{margin:0 0 4px}.cb-src{color:${MUTED};font-size:10.5px}
.cb-dict summary{cursor:pointer;min-height:32px;display:flex;align-items:center}.cb-dict dl{margin:4px 0 0}.cb-dict dt{font-weight:700;margin-top:6px}.cb-dict dd{margin:0 0 2px}
.cb-state{padding:0 6px;background:${PAPER};border:1px solid ${LINE};line-height:24px}
.cb-side{display:flex;flex-direction:column;gap:12px;min-height:0;overflow:auto}
.cb-card{background:${PAPER};border:1.5px solid ${INK};padding:10px}
.cb-h{display:flex;justify-content:space-between;font-weight:700;margin-bottom:8px}
.cb-tabs{display:none}.cb-none{display:none}
.cb-grid{display:grid;grid-template-columns:1fr 1fr;gap:8px}
.cb-kind{display:flex;flex-direction:column;align-items:center;gap:2px;padding:6px 4px;min-height:44px;border:1px solid ${LINE};background:#fff;touch-action:pan-x pan-y;user-select:none;-webkit-user-select:none}
.cb-kind[aria-pressed=true]{outline:2px solid #2f6fd0;outline-offset:-1px}
.cb-kind:disabled{opacity:.4}
.cb-kind small{color:${MUTED};font-size:10px}
.cb-thumb{position:relative;width:112px;height:104px;pointer-events:none}
.cb-thumb img{position:absolute;max-width:none}
.cb-group{margin:10px 0 4px;color:${MUTED};font-size:10.5px}.cb-group:first-of-type{margin-top:0}
.cb-item{display:flex;width:100%;align-items:center;justify-content:space-between;gap:8px;min-height:44px;padding:4px 8px;margin-bottom:4px;text-align:left;border:1px solid ${LINE};background:#fff;touch-action:pan-y;user-select:none;-webkit-user-select:none}
.cb-item[aria-pressed=true]{outline:2px solid #2f6fd0;outline-offset:-1px}
.cb-item small{color:${MUTED};font-size:10px;white-space:nowrap}
.cb-item:disabled{color:${MUTED};background:transparent}
.cb-dot{display:inline-block;width:9px;height:9px;border-radius:50%;border:1px solid ${INK};margin-right:6px}
.cb-row{display:flex;gap:6px;margin-top:8px}.cb-row .cb-btn{flex:1}
.cb-input{width:100%;box-sizing:border-box;min-height:40px;margin-top:8px;padding:0 8px;border:1.5px solid ${INK};background:${PAPER};font:inherit;color:${INK}}
.cb-help{position:absolute;right:10px;top:62px;width:min(300px,calc(100% - 20px));background:${PAPER};border:1.5px solid ${INK};padding:10px;z-index:3}
.cb-help p{margin:0 0 6px}
.cb-float{position:fixed;z-index:50;pointer-events:none;width:72px;height:66px;transform:translate(-50%,-70%);opacity:.9}
@media (max-width:1023px){.cb{grid-template-columns:1fr;grid-template-rows:minmax(0,1fr) minmax(0,40%)}.cb-side{flex-direction:row;overflow:hidden}.cb-card{flex:1;min-width:0;overflow:auto}}
@media (max-width:767px){.cb{padding:0;gap:0;grid-template-rows:minmax(0,1fr) 214px}.cb-stage{border-width:0 0 1.5px}.cb-side{flex-direction:column;gap:0;overflow:hidden}.cb-card{border:0;flex:1;overflow:auto;padding:8px 10px}.cb-none{display:block}.cb-card.off{display:none}.cb-tabs{display:flex;border-bottom:1px solid ${INK};background:${PAPER}}.cb-tabs button{flex:1;min-height:44px;border:0;background:none}.cb-tabs button[aria-selected=true]{font-weight:700;box-shadow:inset 0 -3px 0 ${INK}}.cb-h{display:none}.cb-bar{gap:3px}.cb-tl{left:5px;right:150px;top:5px;flex-wrap:nowrap}.cb-tl .cb-btn:not(.go){flex:1;min-width:0;padding:0}.cb-tl .cb-state{display:none}.cb-tr{right:5px;top:5px;flex-wrap:nowrap}.cb-tr .cb-btn{min-width:46px;padding:0}.cb-btn{padding:0 9px;font-size:11px}.cb-help{top:56px}.cb-thumb{transform:scale(.8);transform-origin:50% 100%;margin-top:-20px}.cb-kind{flex:0 0 112px;padding:3px}.cb-grid{display:flex}}
`

function Thumb({ kind, box = 112 }: { kind: string; box?: number }) {
  const ls = showcaseLayers(kind).map(l => ({ l, s: spriteOf(l.tex) })).map(({ l, s }) => ({ s, left: l.dx - s.ax, top: l.dy - s.ay - (l.stack ? 16 : 0) }))
  const x0 = Math.min(...ls.map(i => i.left)), y0 = Math.min(...ls.map(i => i.top))
  const w = Math.max(...ls.map(i => i.left + i.s.width)) - x0, h = Math.max(...ls.map(i => i.top + i.s.height)) - y0
  const H = box * 104 / 112, k = Math.min(box / w, H / h)
  return <div className="cb-thumb" style={{ width: box, height: H }}>{ls.map((i, n) => <img key={n} alt="" draggable={false} src={spriteUrl(i.s)} width={i.s.width * k} height={i.s.height * k} style={{ left: (i.left - x0) * k + (box - w * k) / 2, top: (i.top - y0) * k + (H - h * k) }} />)}</div>
}

export type CityMeter = { used: number | null; reserved: number | null; of: number | null }
/** A building's reading, its source, its three utility meters and the buildings it is wired to (by what they stand for). */
export type CityReading = Reading & { source: string; meters?: { power: CityMeter; water: CityMeter; gas: CityMeter }; links?: string[] }
export type Glance = { text: string; tone: Reading['tone']; href?: string }

const TONE_BG: Record<Reading['tone'], string> = { calm: '#dcf2e2', busy: '#ffe6a8', alert: '#ffc9c2', unknown: '#e6e4ee' }

export default function CityBuilder({ items, store, startEditing = false, canBuild = true, initial, onOpen, readingOf, sky, workers, readingsKey = '', glance = [] }: {
  items: readonly SystemItem[]; store: LayoutStore; startEditing?: boolean
  /** The real state of what a building stands for, and where it was read. */
  readingOf?: (binds: string) => CityReading | undefined
  /** The satellites in orbit and the legs a real run moves along. */
  sky?: Sky
  /** Each seat that reported, a worker at its building; undefined when not read. */
  workers?: readonly Worker[]
  /** Changes whenever the readings do. */
  readingsKey?: string
  /** One line each for the questions a look should answer. */
  glance?: readonly Glance[]
  /** Whether this viewer may build; without it the city is only looked at. */
  canBuild?: boolean
  /** The arrangement shown until one has been saved. */
  initial?: () => Doc
  onOpen?: (it: SystemItem | undefined, p: Placed) => void
}) {
  const host = useRef<HTMLDivElement>(null), scene = useRef<BuilderScene | null>(null), doc = useRef<Doc>(emptyDoc())
  const [, bump] = useState(0), touch = useCallback(() => bump(v => v + 1), [])
  const [loaded, setLoaded] = useState(false), [editing, setEditing] = useState(false), [tool, setToolS] = useState<Tool>('select')
  const [armed, setArmedS] = useState<Armed>(null), [pendingBind, setPendingBind] = useState<string | null>(null)
  const [zoneSel, setZoneSel] = useState<number | null>(null)
  const [selected, setSelected] = useState<string | null>(null), [tab, setTab] = useState<Tab>('buildings'), [help, setHelp] = useState(false)
  const [saved, setSaved] = useState(''), [savedAt, setSavedAt] = useState<string | null>(null), [saveError, setSaveError] = useState('')
  const [carry, setCarry] = useState<{ a: Armable; x: number; y: number } | null>(null)
  const byBinds = useMemo(() => new Map(items.map(i => [i.binds, i])), [items])
  const itemsRef = useRef(byBinds); itemsRef.current = byBinds
  // The names and colours arrive after the city is drawn: draw its tags and rockets again when they do.
  useEffect(() => { scene.current?.refresh() }, [byBinds])
  const openRef = useRef(onOpen); openRef.current = onOpen
  const readRef = useRef(readingOf); readRef.current = readingOf
  const skyRef = useRef(sky); skyRef.current = sky
  const workersRef = useRef(workers); workersRef.current = workers
  const [info, setInfo] = useState<Placed | null>(null)
  useEffect(() => { scene.current?.setLinks(info?.binds ? [info.binds, ...(readRef.current?.(info.binds)?.links ?? [])] : []) }, [info, readingsKey])
  useEffect(() => { scene.current?.refreshReadings() }, [readingsKey])
  const side = useRef<HTMLDivElement>(null), picker = useRef<HTMLInputElement>(null)
  const [importError, setImportError] = useState(false)
  useEffect(() => { if (selected) side.current?.scrollTo({ top: 0 }) }, [selected])

  useEffect(() => {
    let dead = false, game: { destroy: (b: boolean) => void } | null = null, ro: ResizeObserver | null = null
    void (async () => {
      const map = await store.load().catch(() => null)
      const d = (map && fromTiled(map)) || initial?.() || emptyDoc()
      if (dead || !host.current) return
      doc.current = d
      const blank = canBuild && (startEditing || (d.roads.size === 0 && d.parts.length === 0))
      setSaved(JSON.stringify(toTiled(d))); setEditing(blank); setLoaded(true)
      const [Phaser, { BuilderScene: Scene }] = await Promise.all([import('phaser'), import('./builderScene')])
      if (dead || !host.current) return
      const el = host.current, dpr = Math.min(3, window.devicePixelRatio || 1)
      const g = new Phaser.Game({ type: Phaser.AUTO, parent: el, backgroundColor: '#f3efe6', banner: false, audio: { noAudio: true }, render: { antialias: true }, scale: { mode: Phaser.Scale.NONE, width: el.clientWidth * dpr, height: el.clientHeight * dpr, zoom: 1 / dpr } })
      game = g
      g.scene.add('builder', Scene, true, {
        doc: d, editing: blank, dpr,
        nameOf: (b: string) => itemsRef.current.get(b)?.name, colorOf: (b: string) => itemsRef.current.get(b)?.color,
        onChange: touch,
        onSelect: (id: string | null) => { setSelected(id); if (id) setTab('selected') },
        onZoneSelect: (i: number | null) => { setZoneSel(i); if (i != null) setTab('selected') },
        onPlaced: () => { setArmedS(null); setPendingBind(null); setToolS('select') },
        readingOf: (b: string) => readRef.current?.(b) as never,
        skyOf: () => skyRef.current,
        workersOf: () => workersRef.current,
        onOpen: (p: Placed) => setInfo(p),
        onReady: () => { scene.current = g.scene.getScene('builder') as BuilderScene; touch() },
      })
      ro = new ResizeObserver(() => { if (el.clientWidth && el.clientHeight) g.scale.resize(el.clientWidth * dpr, el.clientHeight * dpr) })
      ro.observe(el)
    })()
    return () => { dead = true; ro?.disconnect(); game?.destroy(true); scene.current = null }
  }, [store, touch, startEditing, canBuild, initial])

  const d = doc.current
  const current = JSON.stringify(toTiled(d)), dirty = loaded && current !== saved
  const placed = new Set(d.parts.map(p => p.binds).filter(Boolean))
  const unplaced = items.filter(i => !placed.has(i.binds))
  const part = d.parts.find(p => p.id === selected)
  const partKind = part && kindOf(part.kind)
  const canBind = partKind ? unplaced.filter(i => accepts(partKind, i)) : []
  const pending = pendingBind ? byBinds.get(pendingBind) : undefined

  const arm = (a: Armed) => { setArmedS(a); scene.current?.setArmed(a); if (a) setToolS('select') }
  const setTool = (t: Tool) => { setToolS(t); setArmedS(null); setPendingBind(null); scene.current?.setTool(t); if (t === 'zone') setTab('selected') }
  const zone = zoneSel != null ? d.zones?.[zoneSel] : undefined
  const zonesChanged = () => { scene.current?.zonesChanged(); touch() }
  const renameZone = (name: string) => { if (zone && name.trim()) { zone.name = name.trim().slice(0, 32); zone.tint = zoneTint(zone.name); zonesChanged() } }
  const removeZone = () => { if (zone && d.zones && zoneSel != null) { d.zones.splice(zoneSel, 1); scene.current?.selectZone(null); zonesChanged() } }
  const pickKind = (k: CatalogKind) => {
    const binds = pending && accepts(k, pending) ? pending.binds : ''
    if (armed?.kind === k.kind && armed.binds === binds) { arm(null); setPendingBind(null) } else arm({ kind: k.kind, binds })
  }
  const pickItem = (it: SystemItem) => {
    if (pendingBind === it.binds) { setPendingBind(null); arm(null); return }
    setPendingBind(it.binds)
    const k = it.natural ? kindOf(it.natural) : undefined
    if (k) arm({ kind: k.kind, binds: it.binds })
    else { arm(null); setTab('buildings') }
  }
  /** A press on a card: a tap arms it; a drag carries the building out onto the land. */
  const press = (e: RP<HTMLButtonElement>, a: Armable | null, tap: () => void) => {
    if (e.button !== 0) return
    e.preventDefault()
    // Captured, so the release reaches us even over the canvas (Phaser listens there too).
    try { e.currentTarget.setPointerCapture(e.pointerId) } catch { /* the pointer is already gone: the window listeners still end the press */ }
    const x0 = e.clientX, y0 = e.clientY, id = e.pointerId
    let going = false
    const move = (m: PointerEvent) => {
      if (m.pointerId !== id || !a) return
      if (!going && Math.hypot(m.clientX - x0, m.clientY - y0) > 10) going = true
      if (going) { setCarry({ a, x: m.clientX, y: m.clientY }); scene.current?.carry(a, m.clientX, m.clientY) }
    }
    const end = (u: PointerEvent) => {
      if (u.pointerId !== id) return
      window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', end); window.removeEventListener('pointercancel', end)
      setCarry(null)
      if (going && a) { if (u.type === 'pointerup') scene.current?.drop(a, u.clientX, u.clientY); setArmedS(null); setPendingBind(null) } else if (u.type === 'pointerup') tap()
    }
    window.addEventListener('pointermove', move); window.addEventListener('pointerup', end); window.addEventListener('pointercancel', end)
  }
  const changed = () => { scene.current?.refresh(); touch() }
  const bind = (binds: string) => { if (part) { part.binds = binds; changed() } }
  const remove = () => { if (part) { d.parts.splice(d.parts.indexOf(part), 1); scene.current?.select(null); changed() } }
  const grow = () => { if (expand(d)) { scene.current?.refresh(true); touch() } }
  const save = async () => {
    const m = toTiled(d)
    try { await store.save(m); setSaved(JSON.stringify(m)); setSaveError(''); setSavedAt(new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })) } catch (e) { setSaveError(e instanceof Error ? e.message : 'Could not save') }
  }
  const exportFile = () => {
    const url = URL.createObjectURL(new Blob([layoutFileText(d)], { type: 'application/json' }))
    const a = document.createElement('a')
    a.href = url; a.download = LAYOUT_FILE; a.click()
    URL.revokeObjectURL(url)
  }
  /** An imported layout replaces the working copy; it is kept only once saved. */
  const importFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (!file) return
    const next = parseLayoutFile(await file.text())
    setImportError(!next)
    if (!next) return
    doc.current = next
    setSelected(null); setArmedS(null); setPendingBind(null)
    scene.current?.setDoc(next)
    if (!editing) toggleEdit(true)
    touch()
  }
  const toggleEdit = (on: boolean) => { setEditing(on); scene.current?.setEditing(on); setArmedS(null); setPendingBind(null); setToolS('select'); scene.current?.setTool('select'); if (!on) setSelected(null) }

  const groups = (['Stages', 'Nodes', 'Domains', 'Release lanes', 'Suppliers', 'Law'] as const).map(g => [g, unplaced.filter(i => i.group === g)] as const).filter(([, l]) => l.length)
  const kindAccepted = (k: CatalogKind) => !pending || accepts(k, pending)

  return (
    <div className="cb" style={editing ? undefined : { gridTemplateColumns: '1fr', gridTemplateRows: '1fr' }}>
      <style>{CSS}</style>
      <div className="cb-stage">
        <div ref={host} className="cb-canvas" data-testid="city-land" />
        <div className="cb-bar cb-tl">
          {editing ? <>
            {(['select', 'road', 'erase', 'zone'] as const).map(t => <button key={t} className="cb-btn" aria-pressed={tool === t && !armed} onClick={() => setTool(t)}>{t === 'select' ? 'Move' : t === 'road' ? 'Road' : t === 'erase' ? 'Erase' : 'Zone'}</button>)}
            <button className="cb-btn" onClick={grow} disabled={d.width >= LAND_MAX}>Land +</button>
            <span className="cb-state">{d.width}×{d.height}</span>
          </> : <>
            {canBuild && <button className="cb-btn go" onClick={() => toggleEdit(true)}>Build</button>}
            {glance.map(g => <a key={g.text} className="cb-glance" href={g.href} style={{ background: TONE_BG[g.tone] }}>{g.text}</a>)}
          </>}
        </div>
        <div className="cb-bar cb-tr">
          {editing && <button className="cb-btn go" onClick={() => void save()} disabled={!dirty}>{saveError ? 'Retry' : dirty ? 'Save' : 'Saved'}</button>}
          {editing && <button className="cb-btn" onClick={() => toggleEdit(false)} disabled={dirty} aria-label="Finish building">Done</button>}
          {editing && saveError && <span className="cb-state" role="alert">{saveError}</span>}
          <button className="cb-btn" aria-label="Help" aria-expanded={help} onClick={() => setHelp(h => !h)}>?</button>
        </div>
        {info && !editing && (() => {
          const it = byBinds.get(info.binds), r = info.binds ? readingOf?.(info.binds) : undefined
          return <div className="cb-sheet" role="dialog" aria-label="Building">
            <h3>{info.binds ? it?.name ?? 'unknown' : 'Not assigned'} <span className="cb-src">{kindOf(info.kind)?.name}</span></h3>
            {r && <p><b style={{ background: TONE_BG[r.tone], padding: '0 4px' }}>{r.word}</b></p>}
            {r?.lines.map(l => <p key={l}>{l}</p>)}
            {(workers ?? []).filter(w => w.target.binds && w.target.binds === info.binds || (!w.target.binds && w.target.kind === info.kind)).flatMap(w => w.lines).map(l => <p key={l}>{l}</p>)}
            {r && <p className="cb-src">{r.source}</p>}
            <div className="cb-row">
              {it?.page && <a className="cb-btn" style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', textDecoration: 'none', color: INK }} href={it.page} onClick={() => openRef.current?.(it, info)}>Open page</a>}
              <button className="cb-btn" onClick={() => setInfo(null)}>Close</button>
            </div>
          </div>
        })()}
        {help && <div className="cb-help">
          <p>Road: drag on the land; corners and crossings form by themselves. Erase: drag over a road.</p>
          <p>Zone: drag a rectangle to draw a district (launch site, town…); tap it to rename or remove it, drag it to move it. Where things stand is only how they are shown.</p>
          <p>Buildings: drag one onto the land, or tap it and tap the land. Drag a placed building to move it; tap it to choose what it stands for.</p>
          <p>Unplaced lists what exists in the system and is not on the land yet. Nothing is placed for you.</p>
          <p>Zoom with the wheel or two fingers; pan with a right-drag, or by dragging empty land in Move.</p>
          <p>Tap a building to open its page. Pinch or scroll to zoom.</p>
          {canBuild && <p>Export keeps the layout as a file; Import opens one.</p>}
          <details className="cb-dict"><summary>What things mean (v{DICTIONARY_VERSION})</summary><dl>
            {DICTIONARY.map(e => <div key={e.id}><dt>{e.metaphor}</dt><dd>{e.means}</dd><dd className="cb-src">{e.tiers.map(t => t.name).join(' · ')}</dd><dd className="cb-src">{e.source}</dd>{e.person && <dd className="cb-src">You: {e.person}</dd>}</div>)}
          </dl></details>
          {savedAt && <p>Last saved {savedAt}.</p>}
          {importError && <p role="alert">That file is not a city layout.</p>}
          {canBuild && <div className="cb-row">
            <button className="cb-btn" onClick={exportFile}>Export</button>
            <button className="cb-btn" onClick={() => picker.current?.click()}>Import</button>
          </div>}
          <input ref={picker} type="file" accept="application/json,.json" hidden aria-label="Import layout file" onChange={e => void importFile(e)} />
        </div>}
      </div>

      {editing && <div className="cb-side" ref={side}>
        <div className="cb-tabs" role="tablist">
          {(['buildings', 'unplaced', 'selected'] as const).map(t => <button key={t} role="tab" aria-selected={tab === t} onClick={() => setTab(t)}>{t === 'buildings' ? 'Buildings' : t === 'unplaced' ? `Unplaced ${unplaced.length}` : 'Selected'}</button>)}
        </div>
        {part && partKind && <section className={`cb-card${tab === 'selected' ? '' : ' off'}`} data-testid="selected">
          <div className="cb-h"><span>{partKind.name}</span></div>
          {partKind.accepts.length > 0 && <div style={{ marginBottom: 6 }}>{part.binds ? byBinds.get(part.binds)?.name ?? part.binds : 'Not assigned'}</div>}
          {canBind.slice(0, 12).map(i => <button key={i.binds} className="cb-item" onClick={() => bind(i.binds)}><span>{i.color && <i className="cb-dot" style={{ background: i.color }} />}{i.name}</span><small>assign</small></button>)}
          <div className="cb-row">{part.binds && <button className="cb-btn" onClick={() => bind('')}>Unassign</button>}<button className="cb-btn" onClick={remove}>Remove</button></div>
        </section>}
        {tool === 'zone' && <section className={`cb-card${tab === 'selected' ? '' : ' off'}`} data-testid="zone">
          <div className="cb-h"><span>{zone ? 'District' : 'Zones'}</span><span>{d.zones?.length ?? 0}</span></div>
          {!zone && <p style={{ color: MUTED, margin: '0 0 6px' }}>Drag on the land to draw a district. Tap one to pick it; drag a picked one to move it. Districts only change how the land looks.</p>}
          {zone && <>
            <div className="cb-row" style={{ flexWrap: 'wrap' }}>{ZONE_NAMES.map(n => <button key={n} className="cb-btn" aria-pressed={zone.name === n} onClick={() => renameZone(n)}>{n}</button>)}</div>
            <input key={`${zoneSel}:${zone.name}`} className="cb-input" aria-label="District name" defaultValue={zone.name} maxLength={32} onKeyDown={e => { if (e.key === 'Enter') renameZone(e.currentTarget.value) }} onBlur={e => renameZone(e.currentTarget.value)} />
            <div className="cb-src">{zone.w}×{zone.h} cells at {zone.x},{zone.y}</div>
            <div className="cb-row"><button className="cb-btn" onClick={removeZone}>Remove district</button></div>
          </>}
        </section>}
        {!part && tool !== 'zone' && <section className={`cb-card cb-none${tab === 'selected' ? '' : ' off'}`}><span style={{ color: MUTED }}>Nothing selected</span></section>}
        <section className={`cb-card${tab === 'buildings' ? '' : ' off'}`}>
          <div className="cb-h"><span>Buildings</span><span>{CATALOG.length}</span></div>
          <div className="cb-grid">
            {CATALOG.map(k => {
              const binds = pending && accepts(k, pending) ? pending.binds : ''
              return <button key={k.kind} className="cb-kind" data-kind={k.kind} aria-pressed={armed?.kind === k.kind} disabled={!kindAccepted(k)} onPointerDown={e => press(e, { kind: k.kind, binds }, () => pickKind(k))}>
                <Thumb kind={k.kind} /><span>{k.name}</span><small>{k.size}×{k.size}</small>
              </button>
            })}
          </div>
        </section>
        <section className={`cb-card${tab === 'unplaced' ? '' : ' off'}`}>
          <div className="cb-h"><span>Unplaced</span><span>{unplaced.length}</span></div>
          {groups.map(([g, list]) => <div key={g}>
            <div className="cb-group">{g}</div>
            {list.map(i => {
              const k = i.natural ? kindOf(i.natural) : undefined, none = i.type === 'other'
              return <button key={i.binds} className="cb-item" data-binds={i.binds} disabled={none} aria-pressed={pendingBind === i.binds}
                onPointerDown={e => { if (!none) press(e, k ? { kind: k.kind, binds: i.binds } : null, () => pickItem(i)) }}>
                <span>{i.color && <i className="cb-dot" style={{ background: i.color }} />}{i.name}</span><small>{none ? 'no building yet' : k ? k.name : 'pick a building'}</small>
              </button>
            })}
          </div>)}
        </section>
      </div>}
      {carry && <div className="cb-float" style={{ left: carry.x, top: carry.y }}><Thumb kind={carry.a.kind} box={72} /></div>}
    </div>
  )
}
