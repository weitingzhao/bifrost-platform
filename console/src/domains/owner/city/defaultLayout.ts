import { emptyDoc, key, type Doc, type PartSize, type Zone } from './builderDoc'

/**
 * The arrangement shown until the Owner has saved one: his zoning plan
 * (2026-11-18, the Owner: a planner keeps the launch site, the payload
 * works, the town and the power yard apart). Four districts with green belts
 * and two arterial roads between them:
 * - the launch site along the coast: horizontal hangar, vertical assembly,
 *   the pad by the sea;
 * - the payload works beside it: the suppliers' docks (the plugins, outside
 *   vendors) deliver to the tenants' payload bays, the bays into the
 *   satellite hall, the hall along the crawlerway to the pad;
 * - the town: mission control, the court, the archive, the dorms;
 * - the power yard: the power station, the cooling-water tower and the
 *   generator sets (the cluster's nodes).
 * It says where things stand and nothing about how they are: names and
 * states are read.
 */
const SIDE = 30
const PARTS: readonly [kind: string, x: number, y: number, size: PartSize, binds: string][] = [
  // The payload works: the vendors' docks deliver to the tenants' bays, the bays into the satellite hall.
  ['supplier', 1, 1, 2, 'plugin:bifrost-platform-plugin'],
  ['supplier', 1, 4, 2, 'plugin:bifrost-platform-plugin-market-data'],
  ['supplier', 1, 7, 2, 'plugin:bifrost-platform-plugin-flex-query'],
  ['bay', 5, 1, 2, 'payload:Trade'],
  ['bay', 5, 5, 2, 'payload:Research'],
  ['satellites', 9, 1, 4, 'stage:satellites'],
  // The launch site along the coast.
  ['hif', 15, 1, 4, 'stage:rocket-ci'],
  ['assembly', 20, 1, 4, 'stage:rocket-stg'],
  ['pad', 25, 1, 4, 'lane:bifrost-deliver-platform-prod'],
  // The town.
  ['control', 1, 11, 4, 'domain:owner'],
  ['court', 6, 11, 4, 'law'],
  ['archive', 11, 11, 2, 'domain:lineage'],
  ['dorm', 1, 18, 2, 'domain:tenant'],
  ['dorm', 4, 18, 2, 'domain:coordination'],
  // The power yard.
  ['power', 15, 11, 4, 'node:gpu-server'],
  ['water', 21, 12, 1, ''],
  ['genset', 15, 18, 2, 'node:ubt-k3s-01'],
  ['genset', 18, 18, 2, 'node:ubt-k3s-02'],
  ['genset', 21, 18, 2, 'node:ubt-k3s-04'],
  ['genset', 24, 18, 2, 'node:ubt-k3s-05'],
  ['genset', 27, 18, 2, 'node:ubt-k3s-07'],
]
const ZONES: readonly Zone[] = [
  { name: 'Payload works', x: 0, y: 0, w: 14, h: 9, tint: '#e7e1f1' },
  { name: 'Launch site', x: 14, y: 0, w: 16, h: 9, tint: '#e3e8ee' },
  { name: 'Town', x: 0, y: 10, w: 14, h: 20, tint: '#f1e8d6' },
  { name: 'Power yard', x: 15, y: 10, w: 15, h: 20, tint: '#e9e6da' },
]

export function plannedCity(): Doc {
  const d = emptyDoc(SIDE)
  const row = (y: number, x0: number, x1: number) => { for (let x = x0; x <= x1; x++) d.roads.add(key(x, y)) }
  const col = (x: number, y0: number, y1: number) => { for (let y = y0; y <= y1; y++) d.roads.add(key(x, y)) }
  row(9, 0, SIDE - 1)                    // the service road behind the coast, between the works and the rest
  col(14, 9, SIDE - 1)                   // the arterial between the town and the power yard
  row(16, 0, 13); row(22, 0, 13)         // the town's streets
  row(16, 15, SIDE - 1); row(22, 15, SIDE - 1)   // the power yard's lanes
  d.parts = PARTS.map(([kind, x, y, size, binds], i) => ({ id: `plan-${i}`, kind, x, y, size, binds }))
  d.zones = ZONES.map(z => ({ ...z }))
  return d
}
