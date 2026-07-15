export type DataLayerMigrationPhase = {
  id: string
  step: number
  /** D-C: position in spine done count (step - 1). */
  spineIndex: number
  /** Display prefix in queue / next_task (①..⑦). */
  displayCode: string
  label: string
  repo: string
  verify: string
  blockedBy?: string
}
