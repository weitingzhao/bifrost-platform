/**
 * Blueprint page — three governance zones (Constitution / Spine / Projection).
 * Governance Phase 5 visual layout anchors and navigation.
 */

import {
  GOVERNANCE_LAYER_CONSTITUTION,
  GOVERNANCE_LAYER_PROJECTION,
  GOVERNANCE_LAYER_SPINE,
  GOVERNANCE_LAYERS,
} from './blueprintCatalog'

export const BLUEPRINT_ZONE_VERSION = '2026-07-01'

export const BLUEPRINT_ZONE_ANCHORS = {
  constitution: 'blueprint-zone-constitution',
  spine: 'blueprint-zone-spine',
  projection: 'blueprint-zone-projection',
} as const

export type BlueprintZoneId = keyof typeof BLUEPRINT_ZONE_ANCHORS

export const BLUEPRINT_ZONE_NAV: Array<{
  id: BlueprintZoneId
  layer: typeof GOVERNANCE_LAYER_CONSTITUTION | typeof GOVERNANCE_LAYER_SPINE | typeof GOVERNANCE_LAYER_PROJECTION
  anchor: string
}> = [
  {
    id: 'constitution',
    layer: GOVERNANCE_LAYER_CONSTITUTION,
    anchor: BLUEPRINT_ZONE_ANCHORS.constitution,
  },
  {
    id: 'spine',
    layer: GOVERNANCE_LAYER_SPINE,
    anchor: BLUEPRINT_ZONE_ANCHORS.spine,
  },
  {
    id: 'projection',
    layer: GOVERNANCE_LAYER_PROJECTION,
    anchor: BLUEPRINT_ZONE_ANCHORS.projection,
  },
]

export function blueprintZoneMeta(
  layer: typeof GOVERNANCE_LAYER_CONSTITUTION | typeof GOVERNANCE_LAYER_SPINE | typeof GOVERNANCE_LAYER_PROJECTION,
) {
  return GOVERNANCE_LAYERS.find(row => row.layer === layer)
}
