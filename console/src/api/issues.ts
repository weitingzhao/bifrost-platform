import { authedFetch, pathSegment } from '@/api/client'

/**
 * One business issue, as the crew and the Owner read it (W-31 BI-1 slice 2;
 * DESIGN-crew-intake §5.3, §8): GET /api/v1/agentrt/issues/{id}, which the
 * platform opens to the issue readers only (the operator and admin tokens on
 * its readers list). The Owner reads here what his card on the issue is
 * about: the card carries the issue's number, hash prefixes and neutral
 * options, never its text. Read only: he answers on the card's own page.
 *
 * Fields of the wrong type read as missing, never as a stand-in that looks
 * healthy; an answer that is not an issue at all is an error.
 */

export const ISSUE_ID_PATTERN = /^BI-[1-9][0-9]*$/

export type IssueText = {
  text_id: number
  kind: string
  person?: string
  channel: string
  said_at?: string
  where?: string
  body: string
  sha256: string
  to_requester: boolean
  by: string
  at: string
}

export type IssueAsk = {
  ask_id: number
  purpose: string
  person: string
  bound_text_id: number
  bound_sha256: string
  verdict?: string
  words_text_id?: number
  answered_by?: string
  answer_channel?: string
  answered_at?: string
  superseded_at?: string
}

export type IssueCard = {
  question_id: string
  purpose: string
  decision?: string
  mission_id?: string
  brief_sha256?: string
  design_sha256?: string
  open: boolean
  outcome?: string
}

export type IssueDecision = { decision: string; proceed: string[] }

export type IssueDetail = {
  issueId: string
  product: string
  kind: string
  title: string
  state: string
  waitingOn: string
  conclusion?: string
  priority?: string
  riskClass?: string
  servedPerson?: { id: string; name: string }
  via: { actor: string; channel: string }
  brief?: { textId: number; sha256: string; confirmed: boolean }
  design?: { textId: number; sha256: string; decisions: IssueDecision[] }
  texts: IssueText[]
  asks: IssueAsk[]
  cards: IssueCard[]
}

type Rec = Record<string, unknown>
const rec = (v: unknown): Rec => (v !== null && typeof v === 'object' && !Array.isArray(v) ? (v as Rec) : {})
const recs = (v: unknown): Rec[] => (Array.isArray(v) ? v.filter(x => x !== null && typeof x === 'object' && !Array.isArray(x)) as Rec[] : [])
const str = (v: unknown): string => (typeof v === 'string' ? v : '')
const opt = (v: unknown): string | undefined => (typeof v === 'string' && v !== '' ? v : undefined)
const num = (v: unknown): number => (typeof v === 'number' && Number.isFinite(v) ? v : 0)
const optNum = (v: unknown): number | undefined => (typeof v === 'number' && Number.isFinite(v) ? v : undefined)

/** Reads the platform's answer; an answer without an issue id is refused. */
export function parseIssueDetail(value: unknown): IssueDetail {
  const doc = rec(value)
  const issue = rec(doc.issue)
  const issueId = str(issue.issue_id)
  if (!ISSUE_ID_PATTERN.test(issueId)) throw new Error('issues: the answer is not an issue')
  const served = rec(issue.served_person)
  const via = rec(issue.via)
  const brief = rec(issue.brief)
  const design = rec(issue.design)
  return {
    issueId,
    product: str(issue.product),
    kind: str(issue.kind),
    title: str(issue.title),
    state: str(issue.state),
    waitingOn: str(issue.waiting_on),
    conclusion: opt(issue.conclusion),
    priority: opt(issue.priority),
    riskClass: opt(issue.risk_class),
    servedPerson: str(served.id) !== '' ? { id: str(served.id), name: str(served.name) } : undefined,
    via: { actor: str(via.actor), channel: str(via.channel) },
    brief: str(brief.sha256) !== '' ? { textId: num(brief.text_id), sha256: str(brief.sha256), confirmed: brief.confirmed === true } : undefined,
    design:
      str(design.sha256) !== ''
        ? {
            textId: num(design.text_id),
            sha256: str(design.sha256),
            decisions: recs(design.decisions).map(d => ({
              decision: str(d.decision),
              proceed: Array.isArray(d.proceed) ? d.proceed.filter((x): x is string => typeof x === 'string') : [],
            })),
          }
        : undefined,
    texts: recs(doc.texts).map(t => ({
      text_id: num(t.text_id),
      kind: str(t.kind),
      person: opt(t.person),
      channel: str(t.channel),
      said_at: opt(t.said_at),
      where: opt(t.where),
      body: str(t.body),
      sha256: str(t.sha256),
      to_requester: t.to_requester === true,
      by: str(t.by),
      at: str(t.at),
    })),
    asks: recs(doc.asks).map(a => ({
      ask_id: num(a.ask_id),
      purpose: str(a.purpose),
      person: str(a.person),
      bound_text_id: num(a.bound_text_id),
      bound_sha256: str(a.bound_sha256),
      verdict: opt(a.verdict),
      words_text_id: optNum(a.words_text_id),
      answered_by: opt(a.answered_by),
      answer_channel: opt(a.answer_channel),
      answered_at: opt(a.answered_at),
      superseded_at: opt(a.superseded_at),
    })),
    cards: recs(doc.cards).map(c => ({
      question_id: str(c.question_id),
      purpose: str(c.purpose),
      decision: opt(c.decision),
      mission_id: opt(c.mission_id),
      brief_sha256: opt(c.brief_sha256),
      design_sha256: opt(c.design_sha256),
      open: c.open === true,
      outcome: opt(c.outcome),
    })),
  }
}

export async function fetchIssue(id: string): Promise<IssueDetail> {
  const response = await authedFetch('issue', `/api/v1/agentrt/issues/${pathSegment(id)}`)
  return parseIssueDetail(await response.json())
}
