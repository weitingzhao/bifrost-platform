import { describe, expect, it, vi } from 'vitest'
import { plannedCity } from '../defaultLayout'
import { addRoad, canPlace, crawlerway, depthOrder, emptyDoc, expand, fromTiled, key, placeCheck, roadMask, roadShape, strokeCells, toTiled, type Doc, type Placed } from '../builderDoc'
import { accepts, CATALOG, displayName, kindOf, laneName, layersOf, systemItems } from '../catalog'
import { browserLayoutStore, layoutFileText, parseLayoutFile } from '../layoutStore'
import { platformLayoutStore, platformRequest, SAVED_ELSEWHERE, type LayoutRequest } from '../platformLayoutStore'

// Origin: 2026-11-17, the Owner via the coordinator: he builds the Ops home's
// city himself: bare land, roads dragged on the grid that join by themselves,
// buildings from a catalogue bound to what they stand for, a tray of what is
// not placed, a saved layout (Tiled-compatible) that reopens the same.

const withRoad = (cells: [number, number][]): Doc => { const d = emptyDoc(); for (const [x, y] of cells) addRoad(d, x, y); return d }

describe('roads', () => {
  it('take their shape from their neighbours', () => {
    const d = withRoad([[5, 4], [4, 5], [5, 5], [6, 5], [5, 6], [8, 8], [8, 9], [9, 9]])
    expect(roadShape(roadMask(d, 5, 5))).toBe('cross')
    expect(roadShape(roadMask(d, 5, 4))).toBe('end')
    expect(roadShape(roadMask(d, 8, 9))).toBe('corner')
  })
  it('a fast drag leaves no gap and turns one corner', () => {
    expect(strokeCells([1, 1], [3, 2])).toEqual([[1, 1], [2, 1], [3, 1], [3, 2]])
  })
  it('are not laid off the land or under a building', () => {
    const d = emptyDoc(); d.parts.push({ id: 'a', kind: 'dorm', x: 2, y: 2, size: 2, binds: '' })
    expect(addRoad(d, 2, 3)).toBe(false); expect(addRoad(d, -1, 0)).toBe(false); expect(addRoad(d, 16, 0)).toBe(false)
  })
})

describe('placing', () => {
  it('needs free land for the whole square', () => {
    const d = withRoad([[6, 6]]); d.parts.push({ id: 'a', kind: 'dorm', x: 0, y: 0, size: 2, binds: '' })
    expect(canPlace(d, 2, 0, 4)).toBe(true); expect(canPlace(d, 1, 0, 2)).toBe(false)
    expect(canPlace(d, 4, 4, 4)).toBe(false); expect(canPlace(d, 13, 0, 4)).toBe(false)
    expect(canPlace(d, 0, 0, 2, 'a')).toBe(true)
  })
  it('more land moves nothing already placed', () => {
    const d = withRoad([[1, 1]]); d.parts.push({ id: 'a', kind: 'dorm', x: 3, y: 3, size: 2, binds: '' })
    expect(expand(d)).toBe(true)
    expect([d.width, d.height, d.parts[0].x, d.roads.has(key(1, 1))]).toEqual([24, 24, 3, true])
  })
})

// Origin: 2026-11-18, the coordinator's review of the sample district: buildings touched and hid each other; the Owner still wants four small ones able to make one big one.
describe('room between buildings', () => {
  it('warns when no cell is left, refuses only what does not fit', () => {
    const d = emptyDoc(); d.parts.push({ id: 'a', kind: 'dorm', x: 4, y: 4, size: 2, binds: '' })
    expect(placeCheck(d, 7, 4, 2)).toBe('ok'); expect(placeCheck(d, 6, 4, 2)).toBe('tight'); expect(placeCheck(d, 6, 6, 1)).toBe('tight')
    expect(placeCheck(d, 5, 4, 2)).toBe('blocked'); expect(placeCheck(d, 4, 4, 2, 'a')).toBe('ok')
  })
})

// Origin: 2026-11-17, the Owner: the assembly building's great door opens straight onto the way to the rockets.
describe('the crawlerway', () => {
  const part = (id: string, kind: string, x: number, y: number): Placed => ({ id, kind, x, y, size: 4, binds: '' })
  const site = () => { const d = emptyDoc(24); d.parts.push(part('h', 'assembly', 1, 1), part('p1', 'pad', 8, 1), part('p2', 'pad', 14, 1)); return d }
  it('joins the door to every pad, sharing a trunk', () => {
    const w = crawlerway(site())
    expect([...w.served].sort()).toEqual(['h', 'p1', 'p2'])
    for (const c of ['2,5', '9,5', '15,5']) expect(w.cells.has(c)).toBe(true)
    expect(w.cells.size).toBe(14)
  })
  it('goes round a building set in its way, and is never saved', () => {
    const d = site(); d.parts.push({ id: 'x', kind: 'dorm', x: 5, y: 5, size: 2, binds: '' })
    const w = crawlerway(d)
    expect(w.served.has('p2')).toBe(true); expect(w.cells.has('5,5')).toBe(false)
    expect(JSON.stringify(toTiled(d))).not.toContain('crawler')
  })
  it('crosses a road rather than following it', () => {
    const d = site(); for (let x = 0; x < 24; x++) addRoad(d, x, 5)
    const on = [...crawlerway(d).cells].filter(c => d.roads.has(c))
    expect(on.length).toBeLessThanOrEqual(3)
  })
  it('is not there without the assembly building', () => {
    const d = emptyDoc(24); d.parts.push(part('p', 'pad', 2, 2)); expect(crawlerway(d).cells.size).toBe(0)
  })
})

describe('what stands in front', () => {
  // Origin: the sprite district of 2026-11-17 drew small buildings behind the big one beside them (sorted by x + y).
  it('a small building to the side of a big one is drawn after it', () => {
    const big = { id: 'big', x: 0, y: 0, size: 4 }, small = { id: 'small', x: 4, y: 0, size: 1 }
    expect(depthOrder([small, big]).map(i => i.id)).toEqual(['big', 'small'])
  })
  it('keeps every item, on a mixed row', () => {
    const items = [{ id: 'a', x: 0, y: 4, size: 2 }, { id: 'b', x: 2, y: 0, size: 4 }, { id: 'c', x: 6, y: 1, size: 1 }, { id: 'd', x: 2, y: 4, size: 2 }]
    const order = depthOrder(items).map(i => i.id)
    expect(order).toHaveLength(4)
    expect(order.indexOf('b')).toBeLessThan(order.indexOf('c')); expect(order.indexOf('b')).toBeLessThan(order.indexOf('d')); expect(order.indexOf('a')).toBeLessThan(order.indexOf('d'))
  })
})

describe('the saved layout', () => {
  it('reopens the same, and holds no readings', () => {
    const d = withRoad([[0, 8], [1, 8], [2, 8]])
    d.parts.push({ id: 'p1', kind: 'pad', x: 4, y: 0, size: 4, binds: 'lane:bifrost-deliver-platform-prod' }, { id: 'p2', kind: 'dorm', x: 0, y: 0, size: 2, binds: '' })
    const m = JSON.parse(JSON.stringify(toTiled(d)))
    expect(m.orientation).toBe('isometric')
    expect(m.layers.map((l: { name: string }) => l.name)).toEqual(['roads', 'parts'])
    const back = fromTiled(m)!
    expect([...back.roads].sort()).toEqual([...d.roads].sort()); expect(back.parts).toEqual(d.parts)
  })
  it('refuses what is not a layout, and drops a part that no longer fits', () => {
    expect(fromTiled({ type: 'map', orientation: 'orthogonal', layers: [] })).toBeNull(); expect(fromTiled(null)).toBeNull()
    const m = toTiled(emptyDoc())
    m.layers[1].objects = [{ id: 1, name: 'x', type: 'dorm', x: 15 * 32, y: 0, width: 64, height: 64 }]
    expect(fromTiled(m)!.parts).toEqual([])
  })
})

describe('the catalogue and the tray', () => {
  const items = systemItems([{ id: 'verification', name: 'Verification', facility: { kind: 'assembly-building', page: '#ci' } }, { id: 'compute', facility: { kind: 'server-hall', page: '#x' } }], [{ pipeline: 'bifrost-deliver-platform-prod', repos: ['bifrost-platform'] }, { pipeline: 'bifrost-deliver-stg', repos: ['bifrost-trade-api'] }], ['market-data'])
  const find = (b: string) => items.find(i => i.binds === b)!
  it('a thing goes to the building that is it, and a lane only to a pad', () => {
    expect(find('domain:verification').natural).toBe('assembly'); expect(find('domain:compute').natural).toBeUndefined()
    expect(CATALOG.filter(k => accepts(k, find('lane:bifrost-deliver-stg'))).map(k => k.kind)).toEqual(['pad'])
    expect(CATALOG.filter(k => accepts(k, find('plugin:market-data')))).toEqual([])
    expect(CATALOG.filter(k => accepts(k, find('law'))).map(k => k.kind)).toEqual(['court'])
  })
  it('only platform PROD gets the starship and the chopsticks; a pad with no lane stands empty', () => {
    expect(layersOf('pad', 'lane:bifrost-deliver-platform-prod').map(l => l.tex)).toEqual(['pad:chopsticks', 'starship', 'pad:front'])
    expect(layersOf('pad', 'lane:bifrost-deliver-stg', '#ff8a5c').map(l => l.tex)).toEqual(['pad:plain', 'rocket:#ff8a5c'])
    expect(layersOf('pad', '').map(l => l.tex)).toEqual(['pad:plain']); expect(kindOf('pad')?.size).toBe(4)
  })
})

// Origin: 2026-11-18, the coordinator: the first version keeps the layout in the browser, with a JSON file to carry it between devices.
// Origin: 2026-11-18, the coordinator's review: labels should read alike ("platform prod" beside "Merge & verification").
describe('names on the land', () => {
  it('read in sentence case with environments in capitals', () => {
    expect(laneName({ pipeline: 'bifrost-deliver-platform-prod', repos: ['bifrost-platform'] })).toBe('Platform PROD')
    expect(laneName({ pipeline: 'bifrost-build-ib-gateway', repos: ['bifrost-platform-plugin'] })).toBe('IB gateway')
    expect(displayName('market-data')).toBe('Market data')
  })
})

describe('keeping the layout', () => {
  const memory = () => { const m = new Map<string, string>(); return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => { m.set(k, v) } } }
  it('saves and loads in the browser', async () => {
    const kv = memory(), store = browserLayoutStore('k', () => kv), m = toTiled(withRoad([[1, 1]]))
    expect(await store.load()).toBeNull()
    await store.save(m)
    expect(fromTiled(await store.load())!.roads.has(key(1, 1))).toBe(true)
  })
  it('opens as bare land when the storage is damaged or refused', async () => {
    const kv = memory(); kv.setItem('k', '{not json')
    expect(await browserLayoutStore('k', () => kv).load()).toBeNull()
    expect(await browserLayoutStore('k', () => { throw new Error('blocked') }).load()).toBeNull()
  })
  it('a file exported opens the same; another file is refused', () => {
    const d = withRoad([[2, 3]]); d.parts.push({ id: 'p', kind: 'dorm', x: 5, y: 5, size: 2, binds: 'domain:owner' })
    const back = parseLayoutFile(layoutFileText(d))!
    expect([...back.roads]).toEqual(['2,3']); expect(back.parts).toEqual(d.parts)
    expect(parseLayoutFile('not json')).toBeNull(); expect(parseLayoutFile('{"type":"tileset"}')).toBeNull()
  })
})

// Origin: 2026-11-18, the coordinator chose the platform's versioned store: a save names the version it started from, and the page still works before the table exists.
describe('the layout on the platform', () => {
  const memory = () => { const m = new Map<string, string>(); return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => { m.set(k, v) } } }
  const map = toTiled(withRoad([[1, 1]]))
  const reply = (status: number, body: unknown = {}) => ({ status, json: async () => body })
  it('loads the platform version and saves on from it', async () => {
    const sent: unknown[] = []
    const request: LayoutRequest = async (method, body) => { if (method === 'GET') return reply(200, { version: 3, layout: map }); sent.push(body); return reply(200, { version: 4, layout: map }) }
    const store = platformLayoutStore(request, browserLayoutStore('k', memory))
    expect(await store.load()).toEqual(map)
    await store.save(map); await store.save(map)
    expect(sent.map(b => (b as { base_version: number }).base_version)).toEqual([3, 4])
  })
  it('says so when another device saved first, keeping the work in the browser', async () => {
    const kv = memory(), local = browserLayoutStore('k', () => kv)
    const store = platformLayoutStore(async m => m === 'GET' ? reply(200, { version: 1, layout: map }) : reply(409, { version: 2 }), local)
    await store.load()
    await expect(store.save(map)).rejects.toThrow(SAVED_ELSEWHERE)
    expect(await local.load()).toEqual(map)
  })
  it('keeps the status of a refused request, whatever its error text says', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'the layout was saved elsewhere', version: 2 }), { status: 409 })))
    try { expect((await platformRequest('PUT', {})).status).toBe(409) } finally { vi.unstubAllGlobals() }
  })
  it('does not call a save done when the platform cannot be reached', async () => {
    const store = platformLayoutStore(async () => reply(0), browserLayoutStore('k', memory))
    await expect(store.save(map)).rejects.toThrow('this browser only')
  })
  it('falls back to the browser while the platform has no table', async () => {
    const kv = memory(), local = browserLayoutStore('k', () => kv); await local.save(map)
    const store = platformLayoutStore(async () => reply(503), local)
    expect(await store.load()).toEqual(map)
    await expect(store.save(map)).resolves.toBeUndefined()
  })
})

// Origin: 2026-11-18, the coordinator's review of the sample district: buildings touched and hid each other; the page opens on this plan until the Owner saves his own.
describe('the planned city', () => {
  it('stands apart, on free land, with the crawlerway reaching every pad', () => {
    const d = plannedCity()
    for (const p of d.parts) expect(placeCheck(d, p.x, p.y, p.size, p.id)).toBe('ok')
    const way = crawlerway(d)
    for (const p of d.parts.filter(q => q.kind === 'pad' || q.kind === 'assembly')) expect(way.served.has(p.id)).toBe(true)
    expect(fromTiled(toTiled(d))!.parts).toHaveLength(d.parts.length)
  })
})
