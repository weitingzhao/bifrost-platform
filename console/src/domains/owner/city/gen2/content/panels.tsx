import { useEffect, useState } from 'react'
import type { Panel } from '../frame/content'
import { Button, Dialog, Footer, Glance, Pager, pageOf, Scroll, Select, Tabs, Toolbar, useConfirm, type PanelSize } from '../frame/parts'
import type { Place } from '../frame/places'

/**
 * The panels beside the 2.5D middle (the Owner, 2026-11-19): the middle shows where things stand,
 * the panels say how much, as tables and charts. A panel is a section of the dashboard page for the
 * same place: the dashboard look stacks them as its page; the game look puts them in the rail or
 * the drawer. So a building is designed once, as its sections, and both looks show them.
 *
 * Greybox: each place has the table of what is on it (read from the menu, so real) and one chart
 * slot drawn with sample numbers, marked as such, until the building's own sections are designed.
 */


const KIND_WORD = { town: 'Town', district: 'District', building: 'Building', room: 'Room' } as const

function InsideTable({ place, open }: { place: Place; open: (child: Place) => void }) {
  return <table className="g2-table">
    <thead><tr><th>Name</th><th>Kind</th><th className="num">Size</th><th>Readings</th></tr></thead>
    <tbody>
      {place.children.map(c => (
        <tr key={c.id}>
          <td><button className="g2-link" onClick={() => open(c)}>{c.label}</button></td>
          <td>{KIND_WORD[c.kind]}</td>
          <td className="num">{c.shape.replace('x', '×')}</td>
          <td className="g2-unknown">not wired</td>
        </tr>
      ))}
    </tbody>
  </table>
}

/** A chart slot with sample numbers: a week of bars, drawn to one scale, labelled as a sample. */
function SampleChart() {
  const days = ['Thu', 'Fri', 'Sat', 'Sun', 'Mon', 'Tue', 'Wed']
  const values = [12, 18, 9, 4, 15, 21, 17]
  const max = 25, W = 300, H = 120, pad = 22, bw = (W - pad) / days.length
  return <svg className="g2-chart" viewBox={`0 0 ${W} ${H + 18}`} role="img" aria-label="Sample chart: a week of bars">
    {[0, 10, 20].map(v => {
      const y = H - (v / max) * (H - 8)
      return <g key={v}><line x1={pad} x2={W} y1={y} y2={y} className="grid" /><text x={pad - 4} y={y + 3} className="tick" textAnchor="end">{v}</text></g>
    })}
    {values.map((v, i) => {
      const h = (v / max) * (H - 8)
      return <g key={days[i]}>
        <rect x={pad + i * bw + 4} y={H - h} width={bw - 8} height={h} rx={2} className={i === values.length - 1 ? 'bar last' : 'bar'} />
        <text x={pad + i * bw + bw / 2} y={H + 13} className="tick" textAnchor="middle">{days[i]}</text>
      </g>
    })}
  </svg>
}

/** `open` goes into a district or building and picks a room, as tapping its block does. */
// ---- A sample of every part (the Owner, 2026-11-19: tabs, drop-downs, buttons, menus, dialogs, paging,
// scrolling, and where each goes). Sample rows, said so; nothing here is a reading.

const KINDS = ['deploy', 'alert', 'approval'] as const
const DOT = { deploy: '#8fb6f2', alert: '#f29b8f', approval: '#ffd36b' } as const
const SAMPLE_EVENTS = Array.from({ length: 23 }, (_, i) => ({
  id: i, kind: KINDS[(i * 7) % 3], what: ['platform to STG', 'disk 80% on 02', 'release card', 'research to PROD', 'TWS slot quiet', 'apply manifest'][i % 6],
  at: `${String(9 + Math.floor(i / 4)).padStart(2, '0')}:${String((i * 13) % 60).padStart(2, '0')}`,
}))
const ABOUT = 'g2-parts-about'

function PartsSample({ size }: { size: PanelSize }) {
  const [tab, setTab] = useState('events')
  const [kind, setKind] = useState('all')
  const [page, setPage] = useState(1)
  const [about, setAbout] = useState(false)
  const [refreshed, setRefreshed] = useState<string | null>(null)
  const [confirm, confirmNode] = useConfirm()
  useEffect(() => {
    const on = () => setAbout(true)
    window.addEventListener(ABOUT, on)
    return () => window.removeEventListener(ABOUT, on)
  }, [])
  const rows = SAMPLE_EVENTS.filter(e => kind === 'all' || e.kind === kind)
  const per = size === 'drawer' ? 6 : 10
  const pg = pageOf(rows, page, per)
  const list = (rs: typeof SAMPLE_EVENTS) => <ul className="k-rows">{rs.map(e => <li key={e.id}><i className="k-dot" style={{ background: DOT[e.kind] }} /><b>{e.kind}</b>{e.what}<span>{e.at}</span></li>)}</ul>
  // A card is a glance: the count and the latest three; the filters, tabs and pages wait in the middle.
  if (size === 'card') return <>
    <Glance value={SAMPLE_EVENTS.length} label="events today" note={`${SAMPLE_EVENTS.filter(e => e.kind === 'alert').length} alerts`} />
    {list(SAMPLE_EVENTS.slice(-3).reverse())}
  </>
  return <>
    <Tabs value={tab} onChange={setTab} tabs={[{ id: 'events', label: 'Events' }, { id: 'settings', label: 'Settings' }]} />
    {tab === 'events' ? <>
      <Toolbar
        left={<Select label="Kind" value={kind} onChange={v => { setKind(v); setPage(1) }} options={[{ value: 'all', label: 'All kinds' }, ...KINDS.map(k => ({ value: k, label: k }))]} />}
        right={<Button kind="primary" onClick={() => setRefreshed(new Date().toLocaleTimeString())}>Refresh</Button>} />
      <Scroll max={size === 'middle' ? undefined : 200}>
        {list(pg.rows)}
      </Scroll>
      <Footer><span>Sample rows{refreshed ? ` · refreshed ${refreshed}` : ''}</span><Pager {...pg} onPage={setPage} /></Footer>
    </> : <>
      <Toolbar left={<Select label="Rows per page" value="auto" onChange={() => {}} options={[{ value: 'auto', label: 'Rows: by size' }]} />}
        right={<Button kind="danger" onClick={() => confirm({ title: 'Reset the sample?', body: 'Filters and paging go back to the start. Nothing else changes: these are sample rows.', action: 'Reset', danger: true, onConfirm: () => { setKind('all'); setPage(1); setRefreshed(null); setTab('events') } })}>Reset…</Button>} />
      <p className="g2-unknown" style={{ margin: 0, fontSize: 12 }}>Settings of a real panel go here: filters it remembers, what it counts.</p>
    </>}
    {confirmNode}
    {about && <Dialog title="Where things go in a panel" onClose={() => setAbout(false)} actions={<Button kind="primary" onClick={() => setAbout(false)}>Got it</Button>}>
      <p>Header: title and tabs left; ⋯ and ⤢ right. Toolbar: filters left, actions right. Body scrolls inside. Footer: paging and where the numbers came from. Dialogs: the main action bottom right; anything that resets asks first.</p>
    </Dialog>}
  </>
}

export function panelsOf(place: Place, open: (child: Place) => void): Panel[] {
  const panels: Panel[] = []
  if (place.children.length > 0) panels.push({ id: 'inside', title: `Inside ${place.label}`, dock: 'right', body: <InsideTable place={place} open={open} /> })
  panels.push({ id: 'chart', title: 'Chart slot', sample: true, dock: 'left', body: <SampleChart /> })
  panels.push({
    id: 'parts', title: 'Parts', sample: true, dock: 'left', body: ({ size }) => <PartsSample size={size} />,
    menu: [
      { label: 'Copy a link to this place', onClick: () => { void navigator.clipboard?.writeText(location.href).catch(() => {}) } },
      { label: 'Where things go…', onClick: () => window.dispatchEvent(new Event(ABOUT)) },
    ],
  })
  return panels
}

/** The card of a picked room, as the rail's first panel. */
export function pickedPanel(item: Place): Panel {
  return {
    id: `picked-${item.id}`, title: item.label, dock: 'right',
    body: <div className="g2-picked"><p>{item.question}</p><dl><div><dt>Readings</dt><dd className="g2-unknown">not wired</dd></div></dl></div>,
  }
}
