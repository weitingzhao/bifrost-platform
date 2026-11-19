import type { Panel } from '../frame/content'
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
          <td className="num">{c.size}×{c.size}</td>
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
export function panelsOf(place: Place, open: (child: Place) => void): Panel[] {
  const panels: Panel[] = []
  if (place.children.length > 0) panels.push({ id: 'inside', title: `Inside ${place.label}`, dock: 'right', body: <InsideTable place={place} open={open} /> })
  panels.push({ id: 'chart', title: 'Chart slot', sample: true, dock: 'left', body: <SampleChart /> })
  return panels
}

/** The card of a picked room, as the rail's first panel. */
export function pickedPanel(item: Place): Panel {
  return {
    id: `picked-${item.id}`, title: item.label, dock: 'right',
    body: <div className="g2-picked"><p>{item.question}</p><dl><div><dt>Readings</dt><dd className="g2-unknown">not wired</dd></div></dl></div>,
  }
}
