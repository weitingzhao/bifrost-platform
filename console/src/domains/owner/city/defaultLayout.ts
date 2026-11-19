import { emptyDoc, key, type Doc, type PartSize } from './builderDoc'

/**
 * The arrangement shown until the Owner has saved one: his zoning plan. The
 * rocket's line by the sea (horizontal hangar, vertical assembly, the one pad)
 * and the satellites' hangar inland, joined to the pad by the crawlerway; the
 * seats' dormitories on a block of their own; mission control and the court
 * furthest from the rockets; the power station with the utilities. It says
 * where things stand and nothing about how they are: names and states are read.
 */
const SIDE = 24
const PARTS: readonly [kind: string, x: number, y: number, size: PartSize, binds: string][] = [
  ['hif', 1, 1, 4, 'stage:rocket-ci'],
  ['assembly', 7, 1, 4, 'stage:rocket-stg'],
  ['pad', 14, 1, 4, 'lane:bifrost-deliver-platform-prod'],
  ['water', 20, 2, 1, ''],
  ['archive', 2, 11, 2, 'domain:lineage'],
  ['dorm', 7, 10, 2, 'domain:tenant'],
  ['dorm', 10, 10, 2, 'domain:coordination'],
  ['satellites', 13, 10, 4, 'stage:satellites'],
  ['control', 1, 18, 4, 'domain:owner'],
  ['court', 7, 18, 4, 'law'],
  ['power', 19, 18, 4, 'domain:foundation'],
]

export function plannedCity(): Doc {
  const d = emptyDoc(SIDE)
  for (const y of [8, 16, 22]) for (let x = 0; x < SIDE; x++) d.roads.add(key(x, y))
  for (const x of [6, 12, 18]) for (let y = 8; y <= 22; y++) d.roads.add(key(x, y))
  d.parts = PARTS.map(([kind, x, y, size, binds], i) => ({ id: `plan-${i}`, kind, x, y, size, binds }))
  return d
}
