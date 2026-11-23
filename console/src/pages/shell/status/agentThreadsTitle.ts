/**
 * The section title's count: a number only when it was read (箭10 2026-11-23: "· ?" said nothing). Not read says
 * so; the reason is in the section body (Could not read agent threads: …). Loading is the pending mark.
 */
export function agentThreadsTitle(unreadable: boolean, loading: boolean, midTurn: number): string {
  return `In progress · Agent threads · ${unreadable ? 'not read' : loading ? '…' : midTurn}`
}
