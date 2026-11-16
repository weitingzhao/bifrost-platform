/** Scope row — one logical component in the Bifrost stack. */
export type ScopeRow = {
  tag: string
  component: string
  technology: string
  notes: string
}

/** End-to-end flow row — how a path moves through environments. */
export type FlowRowStatus = 'live' | 'planned' | 'blocked' | 'tbd'

export type FlowRow = {
  path: string
  stage: 'Development' | 'Staging' | 'Production'
  trigger: string
  runtime: string
  dataStore: string
  status: FlowRowStatus
}

export type HardwareRow = {
  id: string
  host: string
  roleCompose: string
  roleK3s: string
}

export type PlatformPhase = {
  id: string
  label: string
  sequence: string
  deliverables: string
}
