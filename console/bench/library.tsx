// The part library, shown (the Owner, 2026-11-19: one way per kind of thing, built from parts; what is
// done well once serves everywhere). Three shelves: the parts, the facilities put together from them,
// and the landmarks brought over from the first generation. Levels here are examples to show what a
// part does, said so; on the map every level comes from a reading.
import { Draw, type Sprite } from '../src/domains/owner/city/gen2/frame/kit'
import { spriteUrl } from '../src/domains/owner/city/gen2/frame/sprites'
import { beacon, dock, doorRow, gatehouse, grille, hall, plaque, roofNumber, signBand, smoke, tallyBoard, tank, toneOf, windowStrip } from '../src/domains/owner/city/gen2/frame/library/parts'
import { deviceSprite, type Form, type Levels } from '../src/domains/owner/city/gen2/frame/devices'
import * as L from '../src/domains/owner/city/gen2/frame/library/landmarks'
import { DICTIONARY } from '../src/domains/owner/city/gen2/frame/library/dictionary'

const HUE = '#f2d27a'
type Item = { name: string; note: string; sprite: Sprite }

const draw = (w: number, d: number, above: number, f: (g: Draw) => void): Sprite => { const g = new Draw(w, d, above); f(g); return g.done() }
const T = toneOf(HUE, 'day', 'online')

const parts: Item[] = [
  { name: 'tank', note: 'Filled to its level: memory (water), disk (fuel). Empty when nothing is read. Example: 30%, 75%, none.', sprite: draw(3, 1, 40, g => { tank(g, 0.5, 0.5, 11, 28, 0.3, '#5aa9f0'); tank(g, 1.5, 0.5, 11, 28, 0.75, '#c98a3a'); tank(g, 2.5, 0.5, 11, 28, null, '#5aa9f0') }) },
  { name: 'smoke', note: 'Puffs from a stack by how busy (CPU); none when not running. Example: 10%, 90%.', sprite: draw(2, 1, 70, g => { for (const [x, f] of [[0.5, 0.1], [1.5, 0.9]] as const) { g.cylinder(x, 0.5, 5, 0, 40, '#c9c3b8', '#a9a397'); smoke(g, x, 0.5, 40, f, true) } }) },
  { name: 'windowStrip', note: 'As many windows lit as it is busy (CPU). Example: 25%, 75%.', sprite: draw(3, 1, 40, g => { g.box(0.1, 0.1, 2.8, 0.6, 26, T.wall); windowStrip(g, 'left', 0.1, 0.7, 1.4, 6, 20, 4, 0.25, T); windowStrip(g, 'left', 1.5, 0.7, 1.4, 6, 20, 4, 0.75, T) }) },
  { name: 'beacon', note: 'The red light on whatever is down.', sprite: draw(1, 1, 40, g => { g.box(0.2, 0.2, 0.6, 0.6, 20, T.wall); beacon(g, 0.5, 0.5, 20, 'down') }) },
  { name: 'hall', note: 'Walls, ribbed siding, the band in the district’s hue, a gable roof with skylights.', sprite: draw(3, 4, 80, g => hall(g, 0.2, 0.2, 2.4, 3.4, 50, T, HUE, 'day')) },
  { name: 'doorRow', note: 'A roller door per unit, its lamp in the unit’s state, crates to the level. Example: 50%, lamps online / standby / down.', sprite: draw(3, 4, 80, g => { hall(g, 0.2, 0.2, 2.2, 3.4, 50, T, HUE, 'day'); doorRow(g, 2.4, 0.2, 3.4, 50, ['online', 'standby', 'down'], 0.5, true, T) }) },
  { name: 'dock', note: 'A loading dock along a face, its edge striped.', sprite: draw(2, 4, 30, g => dock(g, 0.5, 0.2, 0.8, 3.6, 'day')) },
  { name: 'gatehouse', note: 'A small office, its windows lit by CPU. Example: 50%.', sprite: draw(2, 2, 40, g => gatehouse(g, 0.3, 0.3, 1.4, 1.2, T, 0.5)) },
  { name: 'signBand', note: 'A plate across a face with a name on it.', sprite: draw(3, 1, 40, g => { g.box(0.2, 0.1, 2.6, 0.6, 30, T.wall); signBand(g, 'left', 0.2, 0.7, 2.6, 26, 'ubt-k3s-07') }) },
  { name: 'grille', note: 'A radiator grille low on a face.', sprite: draw(1, 3, 40, g => { g.box(0.1, 0.2, 0.7, 2.6, 30, T.wall); grille(g, 'right', 0.8, 2.8, 2.6, 2, 14) }) },
  { name: 'roofNumber', note: 'A number painted large on a roof: who it is, from far off.', sprite: draw(3, 3, 40, g => { g.box(0.2, 0.2, 2.6, 2.6, 20, T.wall); roofNumber(g, 0.3, 0.3, 2.7, 2.7, 20, '04') }) },
  { name: 'tallyBoard', note: 'How many it holds now (a reading); dark when nothing is read. Example: 38 pods.', sprite: draw(1, 3, 40, g => { g.box(0.1, 0.2, 0.7, 2.6, 30, T.wall); tallyBoard(g, 'right', 0.8, 2.8, 1.3, 18, 38, 'pods') }) },
  { name: 'plaque · linux / mac / win', note: 'A system’s mark on a face (the first generation’s marks).', sprite: draw(1, 3, 40, g => { g.box(0.1, 0.2, 0.7, 2.6, 30, T.wall); plaque(g, 'right', 0.8, 2.8, 0.5, 18, 'linux'); plaque(g, 'right', 0.8, 2.8, 1.3, 18, 'mac'); plaque(g, 'right', 0.8, 2.8, 2.1, 18, 'win') }) },
]

const lv = (state: Levels['state'], cpu: number | null, mem: number | null, disk: number | null, vols?: Levels['vols']): Levels => ({ state, cpu, mem, disks: vols ? vols.map(v => v.f) : [disk], vols })
const PLANT_FACE = { roof: '04', sign: 'DATA PRIMARY', tally: { n: 38, what: 'pods', source: 'example' } }
const facility = (form: Form, w: number, d: number, note: string, levels: Levels): Item => ({ name: `${form} · ${levels.state}`, note, sprite: deviceSprite(form, w, d, HUE, 'day', levels, form === 'plant' ? 'ubt-k3s-04' : '', form === 'plant' ? PLANT_FACE : {}) })
const facilities: Item[] = [
  facility('plant', 4, 4, 'A Linux node, designed: its number on the roof, its pool on the sign, pods running on the side board, the Linux mark, windows and smoke for CPU, the water tank for memory, the fuel tank for disk. Example: CPU 40%, memory 60%, disk 50%.', lv('online', 0.4, 0.6, 0.5)),
  facility('plant', 4, 4, 'The same node on standby: cold, dark, no smoke.', lv('standby', null, null, null)),
  facility('plant', 4, 4, 'The same node down: the red beacon on its stack.', lv('down', null, null, null)),
  facility('station', 4, 8, 'The GPU box, asleep on purpose: cold, no steam.', lv('standby', null, null, null)),
  facility('warehouse', 4, 8, 'The NAS: a hall per volume, a door per disk. Example: 3% on three disks; 47% on two, one of them down.', lv('online', 0.05, 0.37, null, [{ f: 0.03, units: ['online', 'online', 'online'] }, { f: 0.47, units: ['online', 'down'] }])),
  facility('exchange', 4, 4, 'A Windows TWS host (next to be designed).', lv('online', 0.3, 0.4, 0.5)),
  facility('dispatch', 2, 2, 'A Mac mini (next to be designed).', lv('online', 0.3, 0.4, 0.5)),
  facility('office', 2, 2, 'A laptop (next to be designed).', lv('unread', null, null, null)),
  facility('tower', 2, 2, 'Network gear, once it is in the topology.', lv('online', null, null, null)),
]

const landmarks: Item[] = [
  ['assembly', 'The assembly building (gen1: CI, where the rocket is built).', L.assembly()],
  ['missionControl', 'Mission control.', L.missionControl()],
  ['launchPad', 'The launch pad, its tower and chopsticks.', L.launchPad()],
  ['booster', 'The booster.', L.booster()],
  ['starship', 'The starship.', L.starship()],
  ['rocket', 'A rocket.', L.rocket()],
  ['ship', 'A ship.', L.ship()],
  ['horizontalHangar', 'The horizontal hangar.', L.horizontalHangar()],
  ['satelliteHangar', 'The satellite hangar.', L.satelliteHangar()],
  ['orbitSatellite', 'A satellite in orbit.', L.orbitSatellite()],
  ['court', 'The court.', L.court()],
  ['powerStation', 'A power station with cooling towers.', L.powerStation()],
  ['generatorSet · linux', 'A machine room in Linux style: orange, three friends round a ring.', L.generatorSet('linux')],
  ['generatorSet · mac', 'In Mac style: silver, the apple.', L.generatorSet('mac')],
  ['generatorSet · win', 'In Windows style: blue, the four-pane window.', L.generatorSet('win')],
  ['archive', 'The archive.', L.archive()],
  ['waterTower', 'A water tower.', L.waterTower()],
  ['codeFactory', 'The code factory.', L.codeFactory()],
  ['moduleBlock', 'A module block.', L.moduleBlock()],
  ['supplierDock', 'A supplier’s dock (gen1: a plugin, an outside vendor).', L.supplierDock()],
  ['payloadBay', 'A payload bay.', L.payloadBay()],
  ['dormitory', 'A dormitory.', L.dormitory()],
].map(([name, note, sprite]) => ({ name: name as string, note: note as string, sprite: sprite as Sprite }))

const shelf = (title: string, about: string, items: Item[]) => `
  <h2>${title}<span class="chip">${items.length}</span></h2>
  <p>${about}</p>
  <div class="grid">${items.map(i => `<div class="card"><div class="art"><img alt="${i.name}" src="${spriteUrl(i.sprite)}"/></div><b>${i.name}</b><span>${i.note}</span></div>`).join('')}</div>`

const encodingText = (e: (typeof DICTIONARY)[number]['encoding']) =>
  e.kind === 'count' ? `count: lit of ${e.n}` : e.kind === 'size' ? `size: ${e.min}–${e.max} for ${e.from >= 1e9 ? `${e.from / 1e9} GB` : e.from}–${e.to >= 1e12 ? `${e.to / 1e12} TB` : e.to >= 1e9 ? `${e.to / 1e9} GB` : e.to} (log)` : e.kind
const dictionary = `
  <h2>Metaphor dictionary<span class="chip">${DICTIONARY.length}</span></h2>
  <p>Which metaphor shows which metric, kept as data: facilities take their encodings from this table, and a metric not in it cannot be drawn. Changing the table changes the city.</p>
  <table class="dict"><thead><tr><th>Metric</th><th>Part</th><th>Encoding</th><th>What you see</th></tr></thead><tbody>
  ${DICTIONARY.map(m => `<tr><td>${m.metric}</td><td>${m.part}</td><td>${encodingText(m.encoding)}</td><td>${m.meaning}</td></tr>`).join('')}
  </tbody></table>`

document.getElementById('root')!.innerHTML = `
  <h1>City part library</h1>
  <p>One way per kind of thing, everywhere in the city: facilities and rooms are put together from these parts, so what is done well once serves everywhere. Levels on this page are examples to show what a part does; on the map every level comes from a reading.</p>
  ${dictionary}
  ${shelf('Parts', 'Each part that shows a level takes it from a reading; with nothing read it stays empty.', parts)}
  ${shelf('Facilities', 'Put together from the parts. The NAS and the Linux power plant are designed; the others are next, one at a time.', facilities)}
  ${shelf('Landmarks from the first generation', 'Brought over as they were, in the same drawing kit, to picture the rest of the system: the release line, the court, the workshops.', landmarks)}
`
