// The unit workshop (the Owner, 2026-11-19): how each unit of the city is made, inside out, one stage
// at a time, each stage shown here for the Owner to confirm before the next. The process is the frame's
// (frame/units/process.ts); the units are content (content/units/). Read-only: confirmations are the
// Owner's words in the thread, recorded in the spec, never a button here.
import { StrictMode, useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { STAGES, stagesOf, unitRulesBroken, type Detail, type StageId, type UnitSpec } from '../src/domains/owner/city/gen2/frame/units/process'
import { ROOM_STANDARDS } from '../src/domains/owner/city/gen2/frame/units/standards'
import { UNITS, UNIT_PLAN, cpuRoomReadings } from '../src/domains/owner/city/gen2/content/units/cpu'
import { memoryRoomReadings } from '../src/domains/owner/city/gen2/content/units/memory'
import { freezersOf, memoryRoom, memoryRoomShape, sectionsOf } from '../src/domains/owner/city/gen2/frame/units/memoryRoom'
import { cpuRoom, cpuRoomShape, cpuSummaryOf, machinesOf } from '../src/domains/owner/city/gen2/frame/units/cpuRoom'
import { readingText } from '../src/domains/owner/city/gen2/frame/library/parts'
import { DICTIONARY, encode, rowsDrawn, rulerOf, setRuler, STANDARD_PARTS } from '../src/domains/owner/city/gen2/frame/library/dictionary'
import { spriteUrl } from '../src/domains/owner/city/gen2/frame/sprites'

type Point = { labels: Record<string, string>; value: number }
type Node = { id: string; label: string; host?: string }
type Reads = { read_at: string; topology: Node[] | null; reads: Record<string, Record<string, Point[] | null>> }

const snap = Object.values(import.meta.glob('./units.local.json', { eager: true, import: 'default' }))[0] as Reads | undefined

/** Live through the platform when it can be reached (the local bench); the archived read otherwise. */
async function readLive(): Promise<Reads | null> {
  try {
    const get = (p: string) => fetch(p).then(r => (r.ok ? r.json() : Promise.reject(new Error(String(r.status)))))
    const topology = (await get('/api/v1/topology?env=prod')).nodes as Node[]
    const reads: Reads['reads'] = {}
    for (const u of UNITS) {
      reads[u.id] = {}
      await Promise.all(u.details.map(async d => { reads[u.id][d.id] = await get(`/api/v1/telemetry/promql?q=${encodeURIComponent(d.query)}`).then(v => v.points as Point[], () => null) }))
    }
    return { read_at: new Date().toISOString(), topology, reads }
  } catch { return null }
}

const fmt = (v: number, unit: Detail['unit']) =>
  unit === 'share' ? `${(v * 100).toFixed(v < 0.1 ? 1 : 0)}%`
  : unit === 'Hz' ? `${(v / 1e9).toFixed(2)} GHz`
  : unit === '°C' ? `${Math.round(v)}°`
  : unit === 'per second' ? (v >= 1000 ? `${(v / 1000).toFixed(1)}k/s` : `${Math.round(v)}/s`)
  : unit === 'load' ? v.toFixed(2)
  : unit === 'bytes' ? (v >= 1e12 ? `${(v / 1e12).toFixed(1)} TB` : v >= 1e9 ? `${(v / 1e9).toFixed(1)} GB` : `${Math.round(v / 1e6)} MB`)
  : unit === 'bytes per second' ? (v >= 1e6 ? `${(v / 1e6).toFixed(1)} MB/s` : `${Math.round(v / 1e3)} kB/s`)
  : unit === 'seconds' ? (Number.isFinite(v) ? `${(v * 1000).toFixed(1)} ms` : '—')
  : unit === 'rpm' ? `${Math.round(v)} rpm`
  : String(Math.round(v))

/** One machine's reading of one detail, in a cell: a value, or many (per core, sensor, mode), or not read. */
function Cell({ d, pts }: { d: Detail; pts: Point[] }) {
  if (pts.length === 0) return <td className="none">not read</td>
  if (d.per === 'unit') return <td>{fmt(pts[0].value, d.unit)}</td>
  if (d.per === 'mode') {
    const by = [...pts].sort((a, b) => b.value - a.value).filter(p => p.value >= 0.0005)
    return <td title={pts.map(p => `${p.labels.mode} ${fmt(p.value, d.unit)}`).join('\n')}>{by.slice(0, 3).map(p => `${p.labels.mode} ${fmt(p.value, d.unit)}`).join(' · ') || 'all ~0'}</td>
  }
  if (d.unit === 'kind') {
    // A kind, not a number: drives by solid state or spinning, arrays by their state.
    const kinds = d.id === 'drives' ? pts.map(p => p.labels.rotational === '1' ? 'HDD' : 'SSD') : pts.map(p => `${p.labels.device} ${p.labels.state}`)
    const tally = [...new Set(kinds)].map(k => d.id === 'drives' ? `${kinds.filter(x => x === k).length} ${k}` : k).join(' · ')
    return <td title={pts.map(p => `${p.labels.device}: ${p.labels.model ?? p.labels.state ?? ''}`).join('\n')}>{tally}</td>
  }
  const vs = pts.map(p => p.value), lo = Math.min(...vs), hi = Math.max(...vs)
  const name = (p: Point) => d.per === 'core' ? `core ${p.labels.cpu}` : p.labels.device ? `${p.labels.device}${p.labels.state ? ' ' + p.labels.state : ''}` : p.labels.mountpoint ?? `${p.labels.chip ?? ''} ${p.labels.sensor ?? ''}`.trim()
  return <td title={pts.map(p => `${name(p)}: ${fmt(p.value, d.unit)}`).join('\n')}>
    <span className="n">{pts.length}×</span> {lo === hi ? fmt(lo, d.unit) : `${fmt(lo, d.unit)}–${fmt(hi, d.unit)}`}
  </td>
}

function DetailsStage({ u, r }: { u: UnitSpec; r: Reads }) {
  const nodes = r.topology ?? []
  const of = (d: Detail, n: Node) => n.host && n.host !== '127.0.0.1' ? (r.reads[u.id]?.[d.id] ?? []).filter(p => p.labels.instance?.split(':')[0] === n.host) : []
  const failed = u.details.filter(d => r.reads[u.id]?.[d.id] == null)
  return <>
    <div className="scroll">
      <table className="matrix">
        <thead><tr><th>Detail</th><th>Per</th><th>Known by</th>{nodes.map(n => <th key={n.id} title={n.label}>{n.id}</th>)}</tr></thead>
        <tbody>
          {u.details.map(d => (
            <tr key={d.id} className={u.chosen?.includes(d.id) ? 'chosen' : ''}>
              <th title={`${d.meaning}\n\n${d.query}`}><b>{d.label}</b><span>{d.meaning}</span></th>
              <td className="per">{d.per}</td>
              <td className={`per ${d.known === 'hardware' ? 'hw' : ''}`}>{d.known === 'hardware' ? 'the hardware' : d.known === 'system' ? 'the system' : ''}</td>
              {nodes.map(n => <Cell key={n.id} d={d} pts={of(d, n)} />)}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
    {failed.length > 0 && <p className="warn">The platform did not answer for: {failed.map(d => d.label).join(', ')}.</p>}
    <p className="hint">Known by: room standard 1 puts in the room only what the hardware itself knows; “the system” marks an event the operating system counts — the advice; you choose. Hover a detail for its query; hover a cell for every core, sensor or mode. Every machine of the district is a column; one nothing reads says “not read” (the two Windows hosts and the MacBook have no node exporter; the GPU box is asleep).</p>
  </>
}

/** The room drawn, for one machine at a time, from its readings; its key beside it. */
/** How a unit's room is drawn for one machine: the picture, and a line on what it holds. */
type RoomDraw = (r: Reads, host: string | undefined) => { sprite: ReturnType<typeof cpuRoom>; ids: string[]; hint: string; count: number }
const ROOM_DRAW: Record<string, RoomDraw> = {
  cpu: (r, host) => {
    const read = cpuRoomReadings(r.reads.cpu as never, host)
    const { out, ids } = rowsDrawn(() => cpuRoom(read))
    return { sprite: out, ids, count: read.cores.length, hint: `${read.cores.length} cores → ${machinesOf(read)} machines → a ${cpuRoomShape(machinesOf(read)).join('×')} room · ${read.temps.length} sensors${read.temps.length ? `, hottest ${Math.round(Math.max(...read.temps.map(t => t.c)))} °C` : ''}` }
  },
  memory: (r, host) => {
    const read = memoryRoomReadings(r.reads.memory as never, host)
    const { out, ids } = rowsDrawn(() => memoryRoom(read))
    const bench = freezersOf(read).map(s => `${s.id} ${readingText(s.bytes, 'bytes')}`).join(' · ')
    return { sprite: out, ids, count: read.total ?? 0, hint: `${readingText(read.total, 'bytes')} → ${sectionsOf(read)} freezers → a ${memoryRoomShape(read).join('×')} room · ${bench || 'nothing read'} · swap ${readingText(read.swapUsed, 'bytes')} of ${readingText(read.swapTotal, 'bytes')}` }
  },
}
const PLAN = [['cpu', 'Workshop', 'CPU'], ['memory', 'Cold store', 'Memory'], ['disk', 'Warehouse', 'Disk']] as const

/** A unit's room drawn for one machine at a time, from its readings; its key beside it. */
function RoomView({ u, r }: { u: UnitSpec; r: Reads }) {
  const nodes = r.topology ?? []
  const [id, setId] = useState(() => nodes.find(n => n.id === 'ubt-k3s-04')?.id ?? nodes[0]?.id)
  const n = nodes.find(x => x.id === id)
  const { sprite, ids, hint, count } = ROOM_DRAW[u.id](r, n?.host && n.host !== '127.0.0.1' ? n.host : undefined)
  const rows = DICTIONARY.filter(m => ids.includes(m.id))
  return <div className="room">
    <div className="plan" aria-label="Where this room is in the factory">
      {PLAN.map(([pid, room, what]) => <div key={pid} className={pid === u.id ? 'on' : ''}><b>{room}</b><span>{what} · {pid === u.id ? 'this room, drawn here' : ROOM_DRAW[pid] ? 'drawn' : 'not drawn yet'}</span></div>)}
      <p>A computer is a factory: these rooms, each made on its own first, are put together into one building once every room is done.</p>
    </div>
    <div className="pick">{nodes.map(x => <button key={x.id} className={x.id === id ? 'on' : ''} onClick={() => setId(x.id)}>{x.id}</button>)}</div>
    <div className="roomgrid">
      {/* Drawn at one scale for every machine, so a bigger unit is a bigger room on the page too. */}
      <img src={spriteUrl(sprite)} alt={`${u.name} room of ${id}`} style={{ width: `${Math.round(sprite.width * 1.3)}px` }} />
      <ul className="key">
        {rows.map(m => <li key={m.id}><b>{m.shows}{m.scope && <i className="scope">{m.scope}</i>}</b><span>{m.meaning}</span>{m.encoding.kind === 'parts' && rulerOf(m.id) !== m.encoding.each && <em className="ruler">Ruler set here: one {m.part} = {m.encoding.unit === 'GB' ? readingText(rulerOf(m.id), 'bytes') : `${rulerOf(m.id)} ${m.encoding.unit}s`} ({Math.ceil(encode(m.id, count) ?? 0)} drawn)</em>}</li>)}
        {rows.length === 0 && <li><span>Nothing read on this machine, so nothing is drawn in its room.</span></li>}
      </ul>
    </div>
    <p className="hint">{hint}</p>
  </div>
}

const PER_WORD = { unit: 'the whole unit', core: 'each core', sensor: 'each sensor (whole chip or one core, by its label)', mode: 'each mode', disk: 'each drive', volume: 'each volume', array: 'each array' } as const

/** The room drafted: each chosen detail as a piece of the facility's equipment, before it is drawn. */
function InsideDraft({ u }: { u: UnitSpec }) {
  const label = (id: string) => u.details.find(d => d.id === id)?.label ?? id
  const groups = [...new Set(u.inside!.map(i => i.where))]
  return <>
    <p className="hint">Drafted, not drawn yet: what each detail becomes in the room, and where it stands. Once you confirm the mapping, the room is drawn from it.</p>
    <div className="scroll">
      <table className="matrix draft">
        <thead><tr><th>Where</th><th>Detail</th><th>Measured over</th><th>Becomes</th><th>How the reading shows</th></tr></thead>
        <tbody>
          {groups.flatMap(g => u.inside!.filter(i => i.where === g).map((i, k) => (
            <tr key={i.detail}>{k === 0 ? <td rowSpan={u.inside!.filter(x => x.where === g).length} className="per">{g}</td> : null}<td>{label(i.detail)}</td><td className="per">{PER_WORD[u.details.find(d => d.id === i.detail)?.per ?? 'unit']}</td><td><b>{i.thing}</b></td><td className="wrap">{i.shows}</td></tr>
          )))}
        </tbody>
      </table>
    </div>
  </>
}

/**
 * The rulers (the Owner, 2026-11-19): a standard part's size is how the city measures, so it can be
 * adjusted here — the pictures redraw, the readings stay. Kept in this browser only (a way of
 * looking, like a panel folded); the defaults are the dictionary's.
 */
const RULERS_KEY = 'bifrost.city.rulers.v1'
const RULER_CHOICES: Record<string, { label: string; v: number }[]> = {
  'core.machines': [1, 2, 4].map(n => ({ label: `${n} core${n > 1 ? 's' : ''}`, v: n })),
  'mem.sections': [1, 2, 4, 8].map(n => ({ label: `${n} GiB`, v: n * 2 ** 30 })),
  'disk.bays': [64, 128, 256, 512, 1024].map(n => ({ label: n >= 1024 ? '1 TB' : `${n} GB`, v: n * 1e9 })),
}
try {
  const saved = JSON.parse(localStorage.getItem(RULERS_KEY) ?? '{}') as Record<string, number>
  for (const [id, v] of Object.entries(saved)) { try { setRuler(id, v) } catch { /* a ruler the table no longer has */ } }
} catch { /* storage unavailable: the defaults */ }

function Rulers({ onChange }: { onChange: () => void }) {
  const set = (id: string, v: number | null) => {
    setRuler(id, v)
    try {
      const saved = JSON.parse(localStorage.getItem(RULERS_KEY) ?? '{}') as Record<string, number>
      if (v == null) delete saved[id]; else saved[id] = v
      localStorage.setItem(RULERS_KEY, JSON.stringify(saved))
    } catch { /* not kept; still applied here */ }
    onChange()
  }
  return <div className="rulers">
    <h2>Rulers</h2>
    <p>One standard part is this much. A different ruler draws a different picture of the same machine; what is read does not change.</p>
    {STANDARD_PARTS().map(m => {
      const e = m.encoding as { each: number }, now = rulerOf(m.id)
      return <label key={m.id}>
        <span>{m.part}</span>
        <select value={now} onChange={ev => { const v = Number(ev.target.value); set(m.id, v === e.each ? null : v) }}>
          {(RULER_CHOICES[m.id] ?? [{ label: String(e.each), v: e.each }]).map(c => <option key={c.v} value={c.v}>{c.label}{c.v === e.each ? ' (default)' : ''}</option>)}
        </select>
      </label>
    })}
  </div>
}

/**
 * The summary (stage 3): each item, what it is made from and how, and its value on every machine — the
 * same computation the console draws from, so this is what each building will show outside.
 */
function CpuSummaryView({ u, r }: { u: UnitSpec; r: Reads }) {
  const nodes = r.topology ?? []
  const label = (id: string) => u.details.find(d => d.id === id)?.label ?? id
  const of = (n: Node) => cpuSummaryOf(cpuRoomReadings(r.reads.cpu as never, n.host && n.host !== '127.0.0.1' ? n.host : undefined))
  const value = (id: string, s: ReturnType<typeof cpuSummaryOf>) =>
    id === 'cores' ? (s.cores ? String(s.cores) : '—')
    : id === 'busy' ? readingText(s.busy, 'share')
    : id === 'clock' ? (s.clock == null ? '—' : `${readingText(s.clock, 'Hz')} / ${readingText(s.top, 'Hz')}`)
    : readingText(s.temp?.c ?? null, '°C')
  return <>
    <p className="hint">The summary is what the console shows: the room sums itself up there, and the same goes outside to its building. One computation for both.</p>
    <div className="scroll">
      <table className="matrix">
        <thead><tr><th>Item</th><th>Made from</th><th>How</th>{nodes.map(n => <th key={n.id}>{n.id}</th>)}</tr></thead>
        <tbody>{u.summary!.map(it => {
          const sums = nodes.map(of)
          return <tr key={it.id}>
            <th><b>{it.label}</b><span>shows through: {DICTIONARY.find(m => m.id === it.row)?.shows}</span></th>
            <td className="per">{it.from.map(label).join(', ')}</td>
            <td className="per">{it.how}</td>
            {sums.map((s, k) => { const v = value(it.id, s); return <td key={nodes[k].id} className={v === '—' ? 'none' : ''}>{v}</td> })}
          </tr>
        })}</tbody>
      </table>
    </div>
  </>
}

/** The room standards every room is held to (frame/units/standards.ts), drawn from the CPU room. */
function Standards() {
  return <div className="standards">
    <h3>Room standards</h3>
    <ol>{ROOM_STANDARDS.map(r => <li key={r.id}><b>{r.rule}</b> <i className={`chk ${r.check}`}>{r.check === 'test' ? 'tested' : 'by eye'}</i><span>{r.how}</span><em>{r.from}</em></li>)}</ol>
  </div>
}

const STATE_WORD = { confirmed: 'Confirmed', open: 'Making now', waiting: 'Waits' } as const

function App() {
  const [r, setR] = useState<Reads | null>(snap ?? null)
  const [live, setLive] = useState(false)
  const [unitId, setUnitId] = useState('cpu')
  const [, setRulerTick] = useState(0)
  useEffect(() => { readLive().then(x => { if (x) { setR(x); setLive(true) } }) }, [])
  const u = UNITS.find(x => x.id === unitId)
  const stages = u ? stagesOf(u) : []
  const open = stages.find(s => s.state === 'open')?.id
  const [stageId, setStageId] = useState<StageId>(() => stagesOf(UNITS[0]).find(s => s.state === 'open')?.id ?? 'details')
  const stage = STAGES.find(s => s.id === stageId)!
  const broken = u ? unitRulesBroken(u) : []

  return <div className="page">
    <header>
      <h1>Unit workshop</h1>
      <p>How each unit of the city is made: from the inside out, one stage at a time. Each stage is shown here and confirmed by the Owner before the next starts; every stage after Details summarises the one before, and shows nothing it lacks.</p>
      <p className="read">{r ? <>{live ? 'Live read' : 'One read, archived in this page'} · {new Date(r.read_at).toLocaleString('en-US', { timeZone: 'America/Chicago', dateStyle: 'medium', timeStyle: 'short' })} Chicago</> : 'Nothing read: the platform cannot be reached from here and no read is archived.'}</p>
    </header>
    <div className="body">
      <nav>
        <h2>Units</h2>
        {UNIT_PLAN.map((p, i) => {
          const spec = UNITS.find(x => x.id === p.id)
          const done = spec ? stagesOf(spec).filter(s => s.state === 'confirmed').length : 0
          return <button key={p.id} className={p.id === unitId ? 'on' : ''} disabled={!spec} onClick={() => { setUnitId(p.id); setStageId(stagesOf(spec!).find(x => x.state === 'open')?.id ?? 'details') }}>
            <span>{i + 1}. {p.name}</span><em>{p.metaphor}</em>
            <i>{spec ? `${done} of ${STAGES.length} confirmed` : 'not started'}</i>
          </button>
        })}
        <Rulers onChange={() => setRulerTick(t => t + 1)} />
      </nav>
      {u && <section>
        <h2>{u.name} <span className="what">{u.what}</span></h2>
        <ol className="stages">
          {stages.map((s, i) => (
            <li key={s.id}><button className={`${s.state}${s.id === stageId ? ' on' : ''}`} onClick={() => setStageId(s.id)}>
              <b>{i + 1}. {STAGES[i].name}</b><span>{STATE_WORD[s.state]}</span>
            </button></li>
          ))}
        </ol>
        <div className="stage">
          <dl>
            <div><dt>Makes</dt><dd>{stage.makes}</dd></div>
            <div><dt>The Owner confirms</dt><dd>{stage.confirms}</dd></div>
            <div><dt>The machine checks</dt><dd><ul>{stage.checks.map(c => <li key={c}>{c}</li>)}</ul></dd></div>
            {u.confirmed[stage.id] && <div><dt>Confirmed</dt><dd>{u.confirmed[stage.id]!.on} — “{u.confirmed[stage.id]!.words}”</dd></div>}
          </dl>
          {broken.length > 0 && <p className="warn">{broken.join(' · ')}</p>}
          {stageId === 'details' && (r ? <DetailsStage u={u} r={r} /> : <p className="warn">Nothing read.</p>)}
          {stageId === 'inside' && ROOM_DRAW[u.id] && r && <RoomView key={u.id} u={u} r={r} />}
          {stageId === 'summary' && u.id === 'cpu' && r && <CpuSummaryView u={u} r={r} />}
          {stageId === 'inside' && <Standards />}
          {stageId === 'inside' && u.inside && <InsideDraft u={u} />}
          {stageId !== 'details' && stages.find(s => s.id === stageId)!.state === 'waiting' && <p className="hint">Waits for {STAGES[STAGES.findIndex(s => s.id === stageId) - 1].name} to be confirmed.</p>}
          {stageId === open && <p className="ask">Now: {stage.confirms} Say it in the thread; it is recorded in the spec with your words, and the next stage opens.</p>}
        </div>
      </section>}
    </div>
  </div>
}

createRoot(document.getElementById('root')!).render(<StrictMode><App /></StrictMode>)
