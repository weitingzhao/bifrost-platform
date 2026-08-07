export type CapabilityGroup = 'rest' | 'project' | 'ws' | 'flat'

export const CAPABILITY_GROUP_LABELS: Record<CapabilityGroup, string> = {
  rest: 'REST API',
  project: 'Project',
  ws: 'WebSocket',
  flat: 'Flat Files',
}

export const CAPABILITY_GROUP_ORDER: CapabilityGroup[] = ['rest', 'ws', 'flat', 'project']

export interface ChecklistRow {
  id: string
  service: string
  group: CapabilityGroup
  description: string
  tierMin: 'starter' | 'developer' | 'business'
  requiresTrades?: boolean
  projectStatus: 'implemented' | 'partial' | 'not-implemented'
  verification: string
  purpose: string
  helpVerification: string
  testHint?: string
}

export type EffectiveServiceStatus = ChecklistRow['projectStatus'] | 'not-on-tier'
