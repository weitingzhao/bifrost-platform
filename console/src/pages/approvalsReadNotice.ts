/** Visible when an approvals read failed. The clock is local, to the second. */
export function approvalsReadFailure(triedAt: number): string {
  const when = new Date(triedAt > 0 ? triedAt : Date.now())
  const pad = (n: number) => String(n).padStart(2, '0')
  const clock = `${pad(when.getHours())}:${pad(when.getMinutes())}:${pad(when.getSeconds())}`
  return `Could not read approvals — last tried ${clock}`
}
