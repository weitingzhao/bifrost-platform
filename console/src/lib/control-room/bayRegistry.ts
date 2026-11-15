export type FlywheelId = 'A' | 'B' | 'coupling'

export type BayDefinition = {
  id: string
  label: string
  flywheel: FlywheelId
  description: string
  /** Matrix target IDs; empty = special logic */
  matrixTargets?: string[]
  /** Match targets whose id starts with any prefix */
  matrixIdPrefixes?: string[]
  /** Use prod env only */
  prodOnly?: boolean
  /** Static unknown lamp until CI wired */
  staticLamp?: 'unknown'
  externalUrlEnv?: string
}
