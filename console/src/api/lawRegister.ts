import { viewerRead } from '@/api/approvals'
import { readEngine, engineUnreachableText } from '@/api/engine'

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
  /** The audit line's latest attribution on the step (plan_step_attributed, platform 2026-11-24; only added): 法, Agent, 工具 or 没核. */
  attribution?: { category: string; cause?: string; destination?: string; version?: string }
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
  /** 现行 under the framework (planstore lawsview.go; the register holds laws only, LAW-1 四, and the view leaves out 废止); absent from an older platform. */
  state?: string
  /** Where the law sits in the framework (level, scope, subject, modality, parent, enforcement…); absent while the law lacks its frame. */
  frame?: { level?: number; scope?: string; subject?: string; modality?: string; parent?: string; enforcement?: string; [more: string]: unknown }
  /** A law's name (LAW-1: laws are written LAW-k, never abbreviated); absent on rows 1 to 85, which had none. */
  code?: string
}

/**
 * The framework of laws as the system holds it (planstore TheFramework, d7ab0b4b8; LAW-1: step one is a
 * frame that can hold laws, shown with no law in it). Every field may be missing from an older platform: the page
 * says unknown, it never fills one in.
 */
export type FrameworkLevel = { level: number; name: string; meaning: string; required: string[]; in_force: number }
export type FrameworkField = { name: string; label: string; meaning: string }
export type LawFramework = {
  version?: string
  source?: string
  levels?: FrameworkLevel[]
  fields?: FrameworkField[]
  subjects?: string[]
  scope_forms?: string[]
  modalities?: string[]
  states?: Record<string, number>
  enforce?: string[]
  between?: string[]
  integrity?: string[]
  stage_note?: string
}

/**
 * The repealed laws waiting to be deleted (planstore LawPurgeQueue, 2026-11-25, LAW-1 s5): numbers and times only, the
 * repealed themselves are not in the register's view. Absent from an older platform.
 */
export type PurgeQueue = { waiting: number; overdue: number; oldest_repealed_at?: string; next_purge_at?: string }

/** GET /api/v1/agentrt/laws as a whole: the framework (null when the platform does not send it yet), the laws and the purge queue. */
export type LawsAnswer = { framework: LawFramework | null; laws: RegisterLaw[]; purge: PurgeQueue | null; via: 'engine' | 'direct' }

/** The engine's name for this read (UI / Engine 2026-11-24: one read, one name; it passes the platform's answer through). */
export const ENGINE_LAWS_PATH = '/engine/v1/agents'
export const ENGINE_LAWS_QUERY = { read: 'laws' } as const
/** Where the laws page reads through the engine, by the platform's viewer_env. PROD has no engine yet. */
export const ENGINE_LAWS: Readonly<Partial<Record<string, boolean>>> = { stg: true }
export const engineLawsOn = (env: string | null | undefined): boolean => {
  const key = (env ?? '').trim().toLowerCase()
  return key !== '' && Object.hasOwn(ENGINE_LAWS, key) && ENGINE_LAWS[key] === true
}

const answerOf = (body: unknown, via: LawsAnswer['via']): LawsAnswer => {
  const b = (body ?? {}) as { framework?: LawFramework | null; laws?: RegisterLaw[]; purge?: PurgeQueue | null }
  return { framework: b.framework ?? null, laws: b.laws ?? [], purge: b.purge ?? null, via }
}

/** The whole answer, through the engine (it says so when it cannot be reached; never a quiet direct read) or directly. */
export async function fetchLawsAnswer(viaEngine: boolean, signal?: AbortSignal): Promise<LawsAnswer> {
  if (viaEngine) {
    const a = await readEngine<unknown>(ENGINE_LAWS_PATH, { query: { ...ENGINE_LAWS_QUERY }, signal })
    if (!a.reached) throw new Error(engineUnreachableText(a))
    if (a.data == null) throw new Error('law register (engine): ' + (a.evidence.map(e => e.detail ?? e.outcome).join('; ') || 'no data'))
    return answerOf(a.data, 'engine')
  }
  const r = await viewerRead('Law register', '/api/v1/agentrt/laws', signal)
  return answerOf(await r.json(), 'direct')
}

export async function fetchLawRegister(signal?: AbortSignal): Promise<RegisterLaw[]> {
  const r = await viewerRead('Law register', '/api/v1/agentrt/laws', signal)
  const body = (await r.json()) as { laws?: RegisterLaw[] }
  return body.laws ?? []
}

