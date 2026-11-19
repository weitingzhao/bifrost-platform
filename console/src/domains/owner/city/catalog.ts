import { CH, CW, type Sprite } from './kit'
import * as L from './landmarks'
import type { PartSize } from './builderDoc'

/**
 * What the Owner can build, and what in the system can be stood for.
 * The catalogue holds only buildings drawn one by one; something in the
 * system whose building is not drawn yet waits in the tray as such. Which
 * things exist is read from the system (domains, release lanes, plugins).
 */

export type CityDomain = { id: string; name?: string; facility?: { kind: string; page: string } }
export type CityLane = { pipeline: string; repos: string[] }

/** What a release lane carries, from the repositories its pipeline clones. */
function payloadOf(lane: CityLane): string {
  const r = lane.repos
  if (/build-research-dagster/.test(lane.pipeline)) return 'research dagster'
  if (r.some(x => x.startsWith('bifrost-trade-'))) return 'Trade'
  if (r.includes('bifrost-platform')) return 'platform'
  if (r.length === 1) return r[0].replace(/^bifrost-(platform-plugin-)?/, '').replace(/^platform-plugin$/, 'ib-gateway')
  return lane.pipeline.replace(/^bifrost-/, '')
}

export type BindType = 'domain' | 'lane' | 'law' | 'stage'
export type CatalogKind = { kind: string; name: string; size: PartSize; height: number; accepts: BindType[] }

export const CATALOG: readonly CatalogKind[] = [
  { kind: 'hif', name: 'Horizontal hangar', size: 4, height: 100, accepts: ['stage', 'domain'] },
  { kind: 'assembly', name: 'Vertical assembly', size: 4, height: 215, accepts: ['stage', 'domain'] },
  { kind: 'satellites', name: 'Satellite hangar', size: 4, height: 130, accepts: ['stage', 'domain'] },
  { kind: 'control', name: 'Mission control', size: 4, height: 110, accepts: ['domain'] },
  { kind: 'pad', name: 'Launch pad', size: 4, height: 250, accepts: ['lane'] },
  { kind: 'dorm', name: 'Dormitory', size: 2, height: 105, accepts: ['domain'] },
  { kind: 'court', name: 'Court', size: 4, height: 140, accepts: ['law'] },
  { kind: 'power', name: 'Power station', size: 4, height: 120, accepts: ['domain'] },
  { kind: 'archive', name: 'Archive', size: 2, height: 88, accepts: ['domain'] },
  { kind: 'water', name: 'Water tower', size: 1, height: 88, accepts: [] },
]
export const kindOf = (kind: string) => CATALOG.find(c => c.kind === kind)

const NATURAL: Record<string, string> = { 'assembly-building': 'assembly', 'mission-control': 'control', 'comms-tower': 'dorm', 'ground-support': 'power', archive: 'archive' }
const STARSHIP_LANE = 'lane:bifrost-deliver-platform-prod'
const PAYLOAD: Record<string, string> = { platform: '#8a6cff', Trade: '#ff8a5c', research: '#41c7c7', 'research dagster': '#2fae9f', 'market-data': '#5aa9ff', 'flex-query': '#f0b429', 'ib-gateway': '#ff6f91' }

export type SystemItem = {
  binds: string; name: string; group: 'Stages' | 'Domains' | 'Release lanes' | 'Suppliers' | 'Law'
  type: BindType | 'other'
  /** The building it naturally is, when that one is drawn. */
  natural?: string
  page?: string; color?: string
}

/** A name as the page shows it: sentence case, words apart, IB and environments in capitals. */
export function displayName(s: string): string {
  const t = s.replace(/-/g, ' ').replace(/^ib\b/i, 'IB')
  return t.charAt(0).toUpperCase() + t.slice(1)
}

/** The satellite a tenant lane carries: its payload's name and colour. */
export function satelliteOf(l: CityLane): { name: string; color: string } {
  const p = payloadOf(l)
  return { name: displayName(p), color: PAYLOAD[p] ?? '#f2d27a' }
}

export function laneName(l: CityLane): string {
  const payload = payloadOf(l)
  const env = /-prod$/.test(l.pipeline) ? ' PROD' : /deliver-(stg|platform)$/.test(l.pipeline) ? ' STG' : ''
  return displayName(payload) + env
}

/** Which line a pipeline belongs to and which step: the rocket is the platform, the satellites are the tenants (the Owner, 2026-11-18). */
export type Line = 'rocket' | 'satellite'
export type Step = 'ci' | 'stg' | 'prod' | 'build'
export function placeOf(l: CityLane): { line: Line; step: Step } {
  const rocket = l.repos.includes('bifrost-platform') || /^bifrost-(land|ci-infra)$/.test(l.pipeline)
  const step: Step = /^bifrost-(ci-|land$)/.test(l.pipeline) ? 'ci' : /-prod$|^bifrost-deliver-research$/.test(l.pipeline) ? 'prod'
    : /(-stg|^bifrost-deliver-platform)$/.test(l.pipeline) ? 'stg' : 'build'
  return { line: rocket ? 'rocket' : 'satellite', step }
}

/** The three stages the buildings of the lines stand for. */
export const STAGES = [
  { binds: 'stage:rocket-ci', name: 'Rocket CI', natural: 'hif', page: '#work?section=ci' },
  { binds: 'stage:rocket-stg', name: 'Rocket STG', natural: 'assembly', page: '#releases' },
  { binds: 'stage:satellites', name: 'Satellites CI and STG', natural: 'satellites', page: '#releases' },
] as const

export function systemItems(domains: readonly (CityDomain & { name?: string })[], lanes: readonly CityLane[], plugins: readonly string[]): SystemItem[] {
  const rocketProd = lanes.filter(l => { const at = placeOf(l); return at.line === 'rocket' && at.step === 'prod' })
  return [
    ...(lanes.length ? STAGES.map((st): SystemItem => ({ binds: st.binds, name: st.name, group: 'Stages', type: 'stage', natural: st.natural, page: st.page })) : []),
    ...domains.map((d): SystemItem => ({ binds: `domain:${d.id}`, name: d.name ?? displayName(d.id), group: 'Domains', type: 'domain', natural: NATURAL[d.facility?.kind ?? ''], page: d.facility?.page })),
    ...rocketProd.map((l): SystemItem => ({ binds: `lane:${l.pipeline}`, name: laneName(l), group: 'Release lanes', type: 'lane', natural: 'pad', page: '#releases', color: PAYLOAD[payloadOf(l)] })),
    ...plugins.map((p): SystemItem => ({ binds: `plugin:${p}`, name: displayName(p), group: 'Suppliers', type: 'other' })),
    { binds: 'law', name: 'The law', group: 'Law', type: 'law', natural: 'court', page: '#crew?section=instruments' },
  ]
}

export const accepts = (k: CatalogKind, it: SystemItem) => it.type !== 'other' && k.accepts.includes(it.type)

/** A drawn layer of a building: its texture, where its anchor sits from the part's corner, and its place in the stack. */
export type Layer = { tex: string; dx: number; dy: number; stack: boolean; order: number }

const iso = (x: number, y: number, z = 0) => ({ x: (x - y) * (CW / 2), y: (x + y) * (CH / 2) - z })

export function layersOf(kind: string, binds: string, color?: string): Layer[] {
  const base = (tex: string, order = 0): Layer => ({ tex, dx: 0, dy: 0, stack: false, order })
  if (kind !== 'pad') return [base(kind)]
  const at = iso(L.PAD_STACK.x, L.PAD_STACK.y, L.PAD_STACK.z)
  const vehicle = (tex: string): Layer => ({ tex, dx: at.x, dy: at.y, stack: true, order: 0.3 })
  if (binds === STARSHIP_LANE) return [base('pad:chopsticks'), vehicle('starship'), base('pad:front', 0.6)]
  if (binds.startsWith('lane:')) return [base('pad:plain'), vehicle(`rocket:${color ?? '#8a6cff'}`)]
  return [base('pad:plain')]
}

const cache = new Map<string, Sprite>()
const make: Record<string, () => Sprite> = {
  assembly: L.assembly, hif: L.horizontalHangar, satellites: L.satelliteHangar, control: L.missionControl, dorm: () => L.dormitory(0), court: L.court, power: L.powerStation, archive: L.archive, water: L.waterTower,
  'pad:chopsticks': () => L.launchPad(true), 'pad:plain': () => L.launchPad(false), 'pad:front': L.launchPadFront, starship: L.starship,
  'd:tree0': () => L.streetTree(0), 'd:tree1': () => L.streetTree(1), 'd:tree2': () => L.streetTree(2),
  'd:lamp': L.streetLamp, 'd:bench': L.parkBench, 'd:billboard': L.billboard, 'd:signal': L.trafficLight,
}
export function spriteOf(tex: string): Sprite {
  let s = cache.get(tex)
  if (!s) { s = tex.startsWith('rocket:') ? L.rocket(tex.slice(7)) : tex.startsWith('sat:') ? L.orbitSatellite(tex.slice(4)) : make[tex](); cache.set(tex, s) }
  return s
}
export const spriteUrl = (s: Sprite) => `data:image/svg+xml;utf8,${encodeURIComponent(s.svg)}`
/** The textures the scene needs before it can draw anything. */
export const baseTextures = () => [...Object.keys(make), ...Object.values(PAYLOAD).map(c => `rocket:${c}`), 'rocket:#8a6cff', ...[...Object.values(PAYLOAD), '#f2d27a'].map(c => `sat:${c}`)]

/** How a kind looks at its best, for the catalogue's card. */
export const showcaseLayers = (kind: string) => layersOf(kind, kind === 'pad' ? STARSHIP_LANE : '')
