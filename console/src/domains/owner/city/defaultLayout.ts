import { emptyDoc, key, type Doc, type PartSize, type Zone } from './builderDoc'

/**
 * The arrangement shown until the Owner has saved one: a compact plan
 * (2026-11-18, the Owner: every entity stays, the layout should be tight
 * enough to take in at a glance; where things stand is only how they are
 * shown). A long strip of land in three bands, one road between them:
 * - along the coast, the satellite campuses (one a tenant, drawn inside
 *   from the readings), the hall where their satellites meet, and the
 *   rocket's line (horizontal hangar, vertical assembly, the pad);
 * - the suppliers' docks (the plugins) and the town (mission control, the
 *   court, the archive, the dorms);
 * - the power yard: the power station, cooling water, the generator sets.
 * The Owner moves, zones and trims it as he likes; names and states are read.
 */
const W = 29, H = 24
const PARTS: readonly [kind: string, x: number, y: number, size: PartSize, binds: string][] = [
  // Along the coast: the tenants' campuses, the hall where their satellites meet, the rocket's line to the pad.
  ['campus', 0, 0, 8, 'tenant:Trade'],
  ['campus4', 9, 0, 4, 'tenant:Research'],
  ['campus4', 9, 5, 4, 'tenant:Finance'],
  ['satellites', 14, 0, 4, 'stage:satellites'],
  ['hif', 14, 5, 4, 'stage:rocket-ci'],
  ['assembly', 19, 0, 4, 'stage:rocket-stg'],
  ['pad', 24, 0, 4, 'lane:bifrost-deliver-platform-prod'],
  // The middle band: the vendors' docks, then the town.
  ['supplier', 0, 11, 2, 'plugin:bifrost-platform-plugin'],
  ['supplier', 3, 11, 2, 'plugin:bifrost-platform-plugin-market-data'],
  ['supplier', 6, 11, 2, 'plugin:bifrost-platform-plugin-flex-query'],
  ['control', 9, 11, 4, 'domain:owner'],
  ['court', 14, 11, 4, 'law'],
  ['archive', 19, 11, 2, 'domain:lineage'],
  ['dorm', 22, 11, 2, 'domain:tenant'],
  ['dorm', 25, 11, 2, 'domain:coordination'],
  // The back band: the power yard.
  ['power', 0, 17, 4, 'node:gpu-server'],
  ['water', 5, 18, 1, ''],
  ['genset', 7, 17, 2, 'node:ubt-k3s-01'],
  ['genset', 10, 17, 2, 'node:ubt-k3s-02'],
  ['genset', 13, 17, 2, 'node:ubt-k3s-04'],
  ['genset', 16, 17, 2, 'node:ubt-k3s-05'],
  ['genset', 19, 17, 2, 'node:ubt-k3s-07'],
  // Off the grid: machines outside the cluster (the Macs, the Windows TWS hosts, the agent host, the NAS).
  ['genset', 7, 21, 2, 'node:mac-mini-1'],
  ['genset', 10, 21, 2, 'node:mac-mini-2'],
  ['genset', 13, 21, 2, 'node:macbook'],
  ['genset', 16, 21, 2, 'node:win11-host'],
  ['genset', 19, 21, 2, 'node:win11-secondary'],
  ['genset', 22, 21, 2, 'node:ubt-k3s-06'],
  ['genset', 25, 21, 2, 'node:UGREEN-NAS'],
]
const ZONES: readonly Zone[] = [
  { name: 'Satellite campuses', x: 0, y: 0, w: 13, h: 10, tint: '#e7e1f1' },
  { name: 'Launch site', x: 13, y: 0, w: 16, h: 10, tint: '#e3e8ee' },
  { name: 'Suppliers', x: 0, y: 11, w: 8, h: 5, tint: '#e0edf0' },
  { name: 'Town', x: 9, y: 11, w: 20, h: 5, tint: '#f1e8d6' },
  { name: 'Power yard', x: 0, y: 17, w: 29, h: 7, tint: '#e9e6da' },
  { name: 'Off the grid', x: 6, y: 21, w: 23, h: 3, tint: '#e2e0e8' },
]

export function plannedCity(): Doc {
  const d = emptyDoc()
  d.width = W; d.height = H
  // One road between the bands: the land is for the buildings.
  for (const y of [10, 16]) for (let x = 0; x < W; x++) d.roads.add(key(x, y))
  d.parts = PARTS.map(([kind, x, y, size, binds], i) => ({ id: `plan-${i}`, kind, x, y, size, binds }))
  d.zones = ZONES.map(z => ({ ...z }))
  return d
}
