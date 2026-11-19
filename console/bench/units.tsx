// The unit workshop (the Owner, 2026-11-19): how each unit of the city is made, inside out, one stage
// at a time, each stage shown here for the Owner to confirm before the next. The process is the frame's
// (frame/units/process.ts); the units are content (content/units/). Read-only: confirmations are the
// Owner's words in the thread, recorded in the spec, never a button here.
import { StrictMode, useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { STAGES, stagesOf, unitRulesBroken, type Detail, type StageId, type UnitSpec } from '../src/domains/owner/city/gen2/frame/units/process'
import { UNITS, UNIT_PLAN } from '../src/domains/owner/city/gen2/content/units/cpu'

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
  : String(Math.round(v))

/** One machine's reading of one detail, in a cell: a value, or many (per core, sensor, mode), or not read. */
function Cell({ d, pts }: { d: Detail; pts: Point[] }) {
  if (pts.length === 0) return <td className="none">not read</td>
  if (d.per === 'machine') return <td>{fmt(pts[0].value, d.unit)}</td>
  if (d.per === 'mode') {
    const by = [...pts].sort((a, b) => b.value - a.value).filter(p => p.value >= 0.0005)
    return <td title={pts.map(p => `${p.labels.mode} ${fmt(p.value, d.unit)}`).join('\n')}>{by.slice(0, 3).map(p => `${p.labels.mode} ${fmt(p.value, d.unit)}`).join(' · ') || 'all ~0'}</td>
  }
  const vs = pts.map(p => p.value), lo = Math.min(...vs), hi = Math.max(...vs)
  const name = (p: Point) => d.per === 'core' ? `core ${p.labels.cpu}` : `${p.labels.chip ?? ''} ${p.labels.sensor ?? ''}`.trim()
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
        <thead><tr><th>Detail</th><th>Per</th>{nodes.map(n => <th key={n.id} title={n.label}>{n.id}</th>)}</tr></thead>
        <tbody>
          {u.details.map(d => (
            <tr key={d.id} className={u.chosen?.includes(d.id) ? 'chosen' : ''}>
              <th title={`${d.meaning}\n\n${d.query}`}><b>{d.label}</b><span>{d.meaning}</span></th>
              <td className="per">{d.per}</td>
              {nodes.map(n => <Cell key={n.id} d={d} pts={of(d, n)} />)}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
    {failed.length > 0 && <p className="warn">The platform did not answer for: {failed.map(d => d.label).join(', ')}.</p>}
    <p className="hint">Hover a detail for its query; hover a cell for every core, sensor or mode. Every machine of the district is a column; one nothing reads says “not read” (the two Windows hosts and the MacBook have no node exporter; the GPU box is asleep).</p>
  </>
}

const STATE_WORD = { confirmed: 'Confirmed', open: 'Making now', waiting: 'Waits' } as const

function App() {
  const [r, setR] = useState<Reads | null>(snap ?? null)
  const [live, setLive] = useState(false)
  const [unitId, setUnitId] = useState('cpu')
  useEffect(() => { readLive().then(x => { if (x) { setR(x); setLive(true) } }) }, [])
  const u = UNITS.find(x => x.id === unitId)
  const stages = u ? stagesOf(u) : []
  const open = stages.find(s => s.state === 'open')?.id
  const [stageId, setStageId] = useState<StageId>('details')
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
          return <button key={p.id} className={p.id === unitId ? 'on' : ''} disabled={!spec} onClick={() => setUnitId(p.id)}>
            <span>{i + 1}. {p.name}</span><em>{p.metaphor}</em>
            <i>{spec ? `${done} of ${STAGES.length} confirmed` : 'not started'}</i>
          </button>
        })}
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
          {stageId !== 'details' && stages.find(s => s.id === stageId)!.state === 'waiting' && <p className="hint">Waits for {STAGES[STAGES.findIndex(s => s.id === stageId) - 1].name} to be confirmed.</p>}
          {stageId === open && <p className="ask">Now: {stage.confirms} Say it in the thread; it is recorded in the spec with your words, and the next stage opens.</p>}
        </div>
      </section>}
    </div>
  </div>
}

createRoot(document.getElementById('root')!).render(<StrictMode><App /></StrictMode>)
