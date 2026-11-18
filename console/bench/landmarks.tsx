/** The catalogue's landmarks one by one, large, to judge each drawing against the reference. Bench only. */
import { createRoot } from 'react-dom/client'
import type { Sprite } from '../src/domains/owner/city/kit'
import * as L from '../src/domains/owner/city/landmarks'

const url = (s: Sprite) => `data:image/svg+xml;utf8,${encodeURIComponent(s.svg)}`
const scale = Number(new URLSearchParams(location.search).get('scale') ?? 2)
const Img = ({ s, x = 0, y = 0 }: { s: Sprite; x?: number; y?: number }) => <img src={url(s)} width={s.width * scale} height={s.height * scale} style={{ position: 'absolute', left: x * scale, top: y * scale }} />
const pad = L.launchPad(), front = L.launchPadFront(), ship = L.starship()
const [bx, by] = [pad.ax + (L.PAD_STACK.x - L.PAD_STACK.y) * 32, pad.ay + (L.PAD_STACK.x + L.PAD_STACK.y) * 16 - L.PAD_STACK.z]
const items: [string, JSX.Element, number, number][] = [
  ['assembly', <Img s={L.assembly()} />, L.assembly().width, L.assembly().height],
  ['mission control', <Img s={L.missionControl()} />, L.missionControl().width, L.missionControl().height],
  ['launch pad', <><Img s={pad} /><Img s={ship} x={bx - ship.ax} y={by - ship.ay - 16} /><Img s={front} /></>, pad.width, pad.height],
  ['dormitory', <Img s={L.dormitory()} />, L.dormitory().width, L.dormitory().height],
  ['court', <Img s={L.court()} />, L.court().width, L.court().height],
  ['power station', <Img s={L.powerStation()} />, L.powerStation().width, L.powerStation().height],
  ['archive', <Img s={L.archive()} />, L.archive().width, L.archive().height],
  ['water tower', <Img s={L.waterTower()} />, L.waterTower().width, L.waterTower().height],
]
createRoot(document.getElementById('root')!).render(<div style={{ display: 'flex', flexWrap: 'wrap', gap: 16, padding: 16, alignItems: 'flex-end' }}>
  {items.map(([n, el, w, h]) => <figure key={n} style={{ margin: 0 }}><div style={{ position: 'relative', width: w * scale, height: h * scale }}>{el}</div><figcaption>{n}</figcaption></figure>)}
</div>)
