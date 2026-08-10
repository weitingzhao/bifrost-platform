/** Pending phase that declares verify_cmd — Agent can auto-verify. */
export function isAutoReadyPhase(phase: {
  status?: string
  verify_cmd?: string
}): boolean {
  const status = (phase.status ?? '').trim().toLowerCase()
  if (status !== 'pending') return false
  return (phase.verify_cmd ?? '').trim() !== ''
}

export function countAutoReadyPhases(
  phases: Array<{ status?: string; verify_cmd?: string }>,
): number {
  return phases.filter(isAutoReadyPhase).length
}

export function hasVerifyCmd(phase: { verify_cmd?: string }): boolean {
  return (phase.verify_cmd ?? '').trim() !== ''
}
