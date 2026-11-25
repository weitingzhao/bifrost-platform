import { viewerRead } from '@/api/approvals'
/**
 * GET /api/v1/agentrt/owner-issues — where the Owner's complaints, pains and needs go (planstore ownerissue.go; Owner
 * 2026-11-25: 「需要有一个地方放用户的吐槽，痛点，需求就应该放在这里」): each issue drawn from what he said, how often,
 * where it goes, and — once a law answers it — whether he said it again after the law. Read-only.
 */
export type OwnerIssue = {
  issue: string
  title: string
  kinds: string[]
  action: 'law-new' | 'law-amend' | 'enforce' | 'rule' | 'drop' | 'conflict' | string
  detail: string
  to?: string
  law_ref?: string
  gap?: string
  status: string
  evidence: string[]
  quotes?: string[]
  times: number
  first_at: string
  last_at: string
  /** The codes of the laws in force that answer it; the issue names its laws, a law never names an issue. */
  laws?: string[]
  law_in_force_at?: string
  round?: number
  version: number
  by: string
  at: string
  /** How many of its messages came after the law went in force; absent while no law answers it. */
  said_after_law?: number
}
/** Whether the issues a law answers eased after it went in force (planstore LawEffects, 2026-11-25). */
export type LawEffect = {
  law: string
  issues: string[]
  times: number
  said_after: number
  per_week_before: number
  per_week_after: number
  verdict: 'not said again' | 'eased' | 'not eased' | 'no law time' | string
}
export type OwnerIssuesAnswer = {
  issues: OwnerIssue[]
  total: number
  by_action: Record<string, number>
  by_status: Record<string, number>
  came_back_after_law: number
  by_law: LawEffect[]
}

export async function fetchOwnerIssues(signal?: AbortSignal): Promise<OwnerIssuesAnswer> {
  const r = await viewerRead('Owner issues', '/api/v1/agentrt/owner-issues', signal)
  const b = (await r.json()) as Partial<OwnerIssuesAnswer>
  return { issues: b.issues ?? [], total: b.total ?? 0, by_action: b.by_action ?? {}, by_status: b.by_status ?? {}, came_back_after_law: b.came_back_after_law ?? 0, by_law: b.by_law ?? [] }
}
