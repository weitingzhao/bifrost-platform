import { emptyDoc, key, type Doc, type PartSize, type Zone } from './builderDoc'

/**
 * The arrangement shown until the Owner has saved one: his zoning plan
 * (2026-11-18, the Owner: a planner keeps the launch site, the payload
 * works, the town and the power yard apart). Four districts with green belts
 * and two arterial roads between them:
 * - the launch site along the coast: horizontal hangar, vertical assembly,
 *   the pad by the sea;
 * - the satellite campuses beside it: one a tenant (its code factories,
 *   payload bay and modules drawn inside from the readings), the suppliers'
 *   docks (the plugins, outside vendors) and the hall where every satellite
 *   joins the crawlerway to the one pad; room left for new campuses;
 * - the town: mission control, the court, the archive, the dorms;
 * - the power yard: the power station, the cooling-water tower and the
 *   generator sets (the cluster's nodes).
 * It says where things stand and nothing about how they are: names and
 * states are read.
 */
const SIDE = 36
const PARTS: readonly [kind: string, x: number, y: number, size: PartSize, binds: string][] = [
  // The satellite campuses: one a tenant, the vendors' docks feeding them, the hall where satellites meet the crawlerway.
  ['campus', 1, 1, 8, 'tenant:Trade'],
  ['campus4', 10, 1, 4, 'tenant:Research'],
  ['campus4', 10, 6, 4, 'tenant:Finance'],
  ['satellites', 15, 1, 4, 'stage:satellites'],
  ['supplier', 1, 11, 2, 'plugin:bifrost-platform-plugin'],
  ['supplier', 4, 11, 2, 'plugin:bifrost-platform-plugin-market-data'],
  ['supplier', 7, 11, 2, 'plugin:bifrost-platform-plugin-flex-query'],
  // The launch site along the coast.
  ['hif', 21, 1, 4, 'stage:rocket-ci'],
  ['assembly', 26, 1, 4, 'stage:rocket-stg'],
  ['pad', 31, 1, 4, 'lane:bifrost-deliver-platform-prod'],
  // The town.
  ['control', 1, 18, 4, 'domain:owner'],
  ['court', 6, 18, 4, 'law'],
  ['archive', 11, 18, 2, 'domain:lineage'],
  ['dorm', 1, 25, 2, 'domain:tenant'],
  ['dorm', 4, 25, 2, 'domain:coordination'],
  // The power yard.
  ['power', 19, 18, 4, 'node:gpu-server'],
  ['water', 25, 19, 1, ''],
  ['genset', 19, 25, 2, 'node:ubt-k3s-01'],
  ['genset', 22, 25, 2, 'node:ubt-k3s-02'],
  ['genset', 25, 25, 2, 'node:ubt-k3s-04'],
  ['genset', 28, 25, 2, 'node:ubt-k3s-05'],
  ['genset', 31, 25, 2, 'node:ubt-k3s-07'],
]
const ZONES: readonly Zone[] = [
  { name: 'Satellite campuses', x: 0, y: 0, w: 20, h: 16, tint: '#e7e1f1' },
  { name: 'Launch site', x: 20, y: 0, w: 16, h: 10, tint: '#e3e8ee' },
  { name: 'Town', x: 0, y: 17, w: 17, h: 19, tint: '#f1e8d6' },
  { name: 'Power yard', x: 18, y: 17, w: 18, h: 19, tint: '#e9e6da' },
]

export function plannedCity(): Doc {
  const d = emptyDoc(SIDE)
  const row = (y: number, x0: number, x1: number) => { for (let x = x0; x <= x1; x++) d.roads.add(key(x, y)) }
  const col = (x: number, y0: number, y1: number) => { for (let y = y0; y <= y1; y++) d.roads.add(key(x, y)) }
  row(10, 0, SIDE - 1)                   // the service road behind the coast and the campuses
  row(16, 0, SIDE - 1)                   // the arterial between the campuses and the rest
  col(17, 10, SIDE - 1)                  // the arterial between the town and the power yard
  row(23, 0, 16); row(23, 18, SIDE - 1)  // the town's street and the power yard's lane
  d.parts = PARTS.map(([kind, x, y, size, binds], i) => ({ id: `plan-${i}`, kind, x, y, size, binds }))
  d.zones = ZONES.map(z => ({ ...z }))
  return d
}
