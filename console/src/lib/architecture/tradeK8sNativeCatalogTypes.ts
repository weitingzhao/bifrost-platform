export type TradeK8sNativeWave = {
  id: string
  wave: string
  /** D-C: position in the spine `done` count. Status is projected from spine, never held here. */
  spineIndex: number
  label: string
  repo: string
  verify: string
  blockedBy?: string
  /** Short summary of what shipped — spec text for the briefing appendix (NOT a progress field). */
  delivered?: string
}
