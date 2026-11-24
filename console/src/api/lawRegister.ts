import { viewerRead } from '@/api/approvals'

/**
 * GET /api/v1/agentrt/laws — the register of laws by number (planapi/laws.go, planstore/lawsview.go): each law as it
 * stands (version, text, status, the card that last changed it) and, for a law an approved card changed, its plan item,
 * how many of its standards are reached, missed and unmeasured, and the steps those are judged on. Read-only.
 */
export type LawStep = {
  plan_node_id: string
  title: string
  /** 达到, 部分, 没达到, 没核; empty when not judged. */
  grade?: string
  version?: string
}

export type LawCounts = { reached: number; missed: number; unmeasured: number }

export type RegisterLaw = {
  law: string
  version: number
  op: string
  said_on?: string
  text: string
  where?: string
  enforced_by?: string
  status: string
  old_wording?: string
  source: string
  approval_id?: string
  at: string
  plan_item?: string
  counts?: LawCounts
  steps?: LawStep[]
}

export async function fetchLawRegister(signal?: AbortSignal): Promise<RegisterLaw[]> {
  const r = await viewerRead('Law register', '/api/v1/agentrt/laws', signal)
  const body = (await r.json()) as { laws?: RegisterLaw[] }
  return body.laws ?? []
}
