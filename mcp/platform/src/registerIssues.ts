import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import { jsonResult } from './platformClient.js'
import {
  ADD_TO_ISSUE_INPUT,
  CLASSIFY_INPUT,
  CLOSE_CHECK_INPUT,
  CLOSE_INPUT,
  DRAFT_BRIEF_INPUT,
  FILE_ISSUE_INPUT,
  ISSUE_DATA_NOTE,
  ISSUE_KEY_RULE,
  LIST_ISSUES_INPUT,
  MERGE_INPUT,
  NOTE_INPUT,
  PUT_DESIGN_INPUT,
  RECORD_REVIEW_INPUT,
  RELAY_INPUT,
  REPLY_INPUT,
  REQUEST_OWNER_INPUT,
  TAKE_ISSUE_INPUT,
  addToIssue,
  fileIssue,
  getIssueFull,
  issueWrite,
  listIssues,
  mergeIssues,
  relayUserResponse,
} from './issueTools.js'

/**
 * The crew's business issue tools (W-31 BI-1 slice 2, DESIGN-crew-intake
 * §2.2). reg is the server's tool registration, already scoped to the
 * bridge's focus; no focus bridge lists them.
 */
export function registerIssueTools(reg: McpServer['tool']): void {
  const w = (tool: string) => issueWrite(tool)

  reg(
    'file_issue',
    `Business issue: bring one for the crew (POST /issues), as submit_issue does for an external agent: the person's own words, your reading and your proposal, kept apart. on_behalf_of person:owner (channel thread), or person:<name> when the Owner relays her words (channel owner_relay), or self for the crew's own proposal or question. Answers BI-n, clarifying. ${ISSUE_KEY_RULE} ${ISSUE_DATA_NOTE}`,
    FILE_ISSUE_INPUT,
    async (input) => jsonResult(await fileIssue(input)),
  )

  reg(
    'add_to_issue',
    `Business issue: add the Owner's words (or his relay of hers), a reading or a proposal to an issue (POST /issues/{id}/additions). ${ISSUE_KEY_RULE} ${ISSUE_DATA_NOTE}`,
    ADD_TO_ISSUE_INPUT,
    async (input) => jsonResult(await addToIssue(input)),
  )

  reg(
    'relay_user_response',
    `Business issue: the Owner's answer to an ask (or his relay of hers), verbatim, with its verdict, to the ask_id it answers (POST /issues/{id}/responses). Only what was said; your own remark goes in agent_note. ${ISSUE_KEY_RULE} ${ISSUE_DATA_NOTE}`,
    RELAY_INPUT,
    async (input) => jsonResult(await relayUserResponse(input)),
  )

  reg(
    'list_issues',
    `Business issues, newest first: by view (new, triage, active, awaiting_user, parked, closed) or state, product, whom they wait on; each with who brought it and how, its work and its receipt. ${ISSUE_DATA_NOTE}`,
    LIST_ISSUES_INPUT,
    async (input) => jsonResult(await listIssues(input)),
  )

  reg(
    'get_issue_full',
    `Business issue BI-n in full: its texts (her words, readings, proposals, brief, design, reports, replies, answers), asks, evidence, work, events and the Owner's cards. ${ISSUE_DATA_NOTE}`,
    { issue_id: z.string().regex(/^BI-[1-9][0-9]*$/) },
    async ({ issue_id }) => jsonResult(await getIssueFull(issue_id)),
  )

  reg(
    'draft_brief',
    `Business issue: write a new brief and ask the person served (for the crew's own proposal, the Owner) to confirm this version; every open ask is superseded. Refused while its work is live or a trial or an answer is asked. ${ISSUE_KEY_RULE} ${ISSUE_DATA_NOTE}`,
    DRAFT_BRIEF_INPUT,
    async (input) => jsonResult(await w('draft_brief')(input)),
  )

  reg(
    'classify_issue',
    `Business issue: the seat's classification of the confirmed brief, risk class (only rises) and category, citing its brief task's report. ${ISSUE_KEY_RULE}`,
    CLASSIFY_INPUT,
    async (input) => jsonResult(await w('classify_issue')(input)),
  )

  reg(
    'put_design',
    `Business issue: the design of the confirmed brief, with the points the Owner decides and the letters under which the work goes on; refused while work is live. ${ISSUE_KEY_RULE}`,
    PUT_DESIGN_INPUT,
    async (input) => jsonResult(await w('put_design')(input)),
  )

  reg(
    'record_review',
    `Business issue: an independent review of the current brief and design, by another tool than the author, round after round (a third only on the Owner's another_round); its conclusion is read from the report's last line. ${ISSUE_KEY_RULE}`,
    RECORD_REVIEW_INPUT,
    async (input) => jsonResult(await w('record_review')(input)),
  )

  reg(
    'record_close_check',
    `Business issue: the seat's check of an answer given (supports or does_not), which a close of resolved or partial rests on. ${ISSUE_KEY_RULE}`,
    CLOSE_CHECK_INPUT,
    async (input) => jsonResult(await w('record_close_check')(input)),
  )

  reg(
    'request_owner',
    `Business issue: the runtime asks the Owner on a decision card, by its own fixed template: the issue's number, the purpose, hash prefixes and neutral options, never a word you write (the card store is read widely; the text is on the issue's page). purpose: priority (two steps: take it on / later / decline, then p1 / p2 / p3), confirm_brief (a brief that asks the Owner), design_decision (decision D<n>, letters 2 to 4), review_dispute (after two review rounds). His answer becomes the issue's evidence by itself. ${ISSUE_KEY_RULE}`,
    REQUEST_OWNER_INPUT,
    async (input) => jsonResult(await w('request_owner')(input)),
  )

  reg(
    'take_issue',
    `Business issue: take it, in one transaction with the mission made for it (work id BI-n) when the take predicate holds by the intake-gates rule; outside the standing envelope the mission waits on the Owner's card. The mission's title, goal and criteria are read by executors: engineering material only, never her words. ${ISSUE_KEY_RULE}`,
    TAKE_ISSUE_INPUT,
    async (input) => jsonResult(await w('take_issue')(input)),
  )

  reg(
    'reply_to_requester',
    `Business issue: write back to the side that brought it: a reply, or an ask (needs_info, check_answer, ready_to_try with the delivery: commitment, mission, env, commits). Its agent reads it with get_updates. ${ISSUE_KEY_RULE} ${ISSUE_DATA_NOTE}`,
    REPLY_INPUT,
    async (input) => jsonResult(await w('reply_to_requester')(input)),
  )

  reg(
    'close_issue',
    `Business issue: close it with a conclusion and the ask it rests on, by the close predicate (resolved and partial need her answer to the newest trial and the seat's latest check supporting it). ${ISSUE_KEY_RULE}`,
    CLOSE_INPUT,
    async (input) => jsonResult(await w('close_issue')(input)),
  )

  reg(
    'park_issue',
    `Business issue: park it, with why and when it comes back (refused while its work is live). ${ISSUE_KEY_RULE}`,
    NOTE_INPUT,
    async (input) => jsonResult(await w('park_issue')(input)),
  )

  reg(
    'resume_issue',
    `Business issue: resume a parked issue at the step it left. ${ISSUE_KEY_RULE}`,
    NOTE_INPUT,
    async (input) => jsonResult(await w('resume_issue')(input)),
  )

  reg(
    'reopen_issue',
    `Business issue: reopen a closed issue (its trials and answers asked before count no more). ${ISSUE_KEY_RULE}`,
    NOTE_INPUT,
    async (input) => jsonResult(await w('reopen_issue')(input)),
  )

  reg(
    'merge_issues',
    `Business issues: merge a duplicate into another of the same person and product, neither ever taken; every word stays with the issue it came with. ${ISSUE_KEY_RULE}`,
    MERGE_INPUT,
    async (input) => jsonResult(await mergeIssues(input)),
  )
}
