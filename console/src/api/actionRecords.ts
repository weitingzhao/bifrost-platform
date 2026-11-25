import { viewerRead } from '@/api/approvals'

/**
 * GET /api/v1/records — the action record (agentrt.action_record, append-only): every direct call
 * of a catalogued action, recorded when it ends. LAW-6 (the Owner, 2026-11-19; was 46): what can be undone
 * runs without a card and is recorded instead; this is where he reads what was done for him.
 * Shape measured on PROD 2026-11-19 (list_records), not taken from the plan's words.
 */
export type ActionRecord = {
  id: number
  at: string
  seat: string
  action: string
  target: string
  reason: string
  /** How to undo it, as the caller wrote it ('' when nothing was written). */
  regret: string
  /** ready (machine-verified), declared (written, unverified), undeclared (nothing written), not_ready. */
  regret_state: string
  law: string
  /** done, refused or failed. */
  outcome: string
  /** What was checked after the action ('' until the caller writes it). */
  check_result: string
  not_verified: string
  params_hash: string
}

export type ActionRecordPage = { records: ActionRecord[]; returned: number; more: boolean }

export async function fetchActionRecords(o: { since: string; limit?: number }): Promise<ActionRecordPage> {
  const q = new URLSearchParams({ since: o.since, limit: String(o.limit ?? 500) })
  const r = await viewerRead('Action record', `/api/v1/records?${q}`)
  const body = (await r.json()) as Partial<ActionRecordPage>
  return { records: body.records ?? [], returned: body.returned ?? 0, more: body.more === true }
}
