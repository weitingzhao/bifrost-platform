import type { CapabilityInfo, CapabilityMatrix } from '@/api/marketDataCapabilities'

export type CapabilityGroups = {
  entitled: CapabilityInfo[]
  planned: CapabilityInfo[]
  unavailable: CapabilityInfo[]
}

/** Split the matrix into the three columns the panel draws; order is the API's. */
export function groupCapabilities(matrix: Pick<CapabilityMatrix, 'capabilities'> | null): CapabilityGroups {
  const groups: CapabilityGroups = { entitled: [], planned: [], unavailable: [] }
  for (const cap of matrix?.capabilities ?? []) {
    if (cap.status === 'entitled') groups.entitled.push(cap)
    else if (cap.status === 'planned') groups.planned.push(cap)
    else groups.unavailable.push(cap)
  }
  return groups
}

/** "needs Options Developer" — the one line a planned capability shows. */
export function upgradeLine(cap: CapabilityInfo): string {
  return cap.requires ? `needs ${cap.requires}` : 'needs a subscription change'
}
