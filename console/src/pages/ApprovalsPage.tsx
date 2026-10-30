import { ApprovalReviewButton } from '@/pages/ApprovalReviewButton'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { Button, DenseTag } from '@bifrost/ui'
import { authedFetch } from '@/api/client'
import {
  approvalFailureStatus,
  approvalIdFromHash,
  clearLegacyApprovalToken,
  fetchApproval,
  fetchApprovalList,
  formatTimeRemaining,
  formatWaited,
  isUnsetDecidedAt,
  postApprovalDecision,
  postApprovalResolve,
  type ApprovalItem,
  type ResolveOutcome,
} from '@/api/approvals'
import { NEEDS_YOU_HREF } from '@/lib/shell/consoleRoutes'
import { ApprovalPlanSteps } from '@/pages/shell/needs-you/ApprovalPlanSteps'
import { approvalsReadFailure } from '@/pages/approvalsReadNotice'
import { CloseUnknownForm } from '@/pages/CloseUnknownForm'
import { ConnectionCheckError, withDecisionContact } from '@/pages/decisionContact'
import { ServerContactStatus } from '@/pages/ServerContactStatus'
import { useDecisionPageResume } from '@/pages/useDecisionPageResume'
import { DECISION_CHANGED } from '@/pages/serverContactNotice'
import {
  closedByYou,
  commandText,
  decisionBanner,
  decisionEnv,
  decisionTitle,
  executionAttempts,
  executionExecutor,
  executionOutcome,
  executionOutputTail,
  ownerTerminal,
  runFromTradeInfra,
  splitReason,
  technicalParams,
  technicalResult,
  tierWords,
  unknownFollowup,
  visibleTimestamp,
  workItems,
} from '@/pages/approvalDecision'
import {
  approvalRequestHref,
  claimedSentence,
  requestRef,
  siblingRequests,
  statusInPlainWords,
} from '@/pages/approvalSiblings'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  DECISION_REJECTED,
  decisionBlockReason,
  scrubSecret,
} from '@/pages/decisionSignIn'
import { NEEDS_YOU_REFRESH_MS } from '@/pages/shell/needs-you/useNeedsYou'
import { ConsoleBuildBanner } from '@/pwa/ConsoleBuildBanner'
import { DECISION_AFTER_RELOAD, NEWER_CONSOLE_BANNER } from '@/pwa/consoleBuild'
import { useConsoleBuildDrift } from '@/pwa/useConsoleBuildDrift'
import { approvalOptionLabel } from '@/api/assessment'
import { isLandingAction } from '@/api/landingGrant'
import { LandingApprovalEvidence } from '@/pages/landing-grant/LandingApprovalEvidence'
import { useLandingMaterial } from '@/pages/landing-grant/useLandingMaterial'
import { AssessmentSection } from '@/pages/question/AssessmentPanel'

function useRecordState<T>(id: string | null, initial: T): [T, (value: T) => void] {
  const [records, setRecords] = useState<Record<string, T>>({})
  const key = id ?? ''
  return [records[key] ?? initial, value => setRecords(previous => ({ ...previous, [key]: value }))]
}

function isFinished(item: ApprovalItem): boolean {
  return ['executed', 'failed', 'rejected', 'expired'].includes(item.status) || item.resolution != null
}

function keepDecision(old: unknown, fresh: unknown): ApprovalItem {
  const previous = old as ApprovalItem | undefined
  const next = fresh as ApprovalItem
  if (previous != null && (
    (isFinished(previous) && (next.status !== previous.status || (previous.resolution != null && next.resolution == null))) ||
    (previous.status !== 'pending' && next.status === 'pending')
  )) return previous
  return next
}

function useDeepLinkId(): string | null {
  const [id, setId] = useState(() => approvalIdFromHash(window.location.hash))
  useEffect(() => {
    const sync = () => setId(approvalIdFromHash(window.location.hash))
    window.addEventListener('hashchange', sync)
    return () => window.removeEventListener('hashchange', sync)
  }, [])
  return id
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 max-w-full">
      <div className="text-xs uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className="break-words">{value || '—'}</div>
    </div>
  )
}

function Prose({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 max-w-full">
      <div className="text-xs uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className="max-w-[65ch] whitespace-pre-wrap break-words" data-prose={label}>
        {value || '—'}
      </div>
    </div>
  )
}

function CopyButton({ text }: { text: string }) {
  const [label, setLabel] = useState('Copy')
  return (
    <Button
      type="button"
      variant="outline"
      className="w-full shrink-0 sm:w-auto"
      onClick={() => {
        void (async () => {
          try {
            await navigator.clipboard.writeText(text)
            setLabel('Copied')
          } catch {
            setLabel('Copy failed')
          }
          window.setTimeout(() => setLabel('Copy'), 2000)
        })()
      }}
    >
      {label}
    </Button>
  )
}

function CodeBox({
  text,
  capped = false,
  marker,
}: {
  text: string
  capped?: boolean
  marker?: 'command' | 'owner'
}) {
  return (
    <div className="flex w-full min-w-0 max-w-full flex-col gap-2">
      <pre
        data-approval-command={marker === 'command' ? '' : undefined}
        data-owner-line={marker === 'owner' ? '' : undefined}
        className={`m-0 w-full min-w-0 max-w-full overflow-auto whitespace-pre font-mono text-xs ${capped ? 'max-h-48' : ''}`}
      >
        {text}
      </pre>
      <CopyButton text={text} />
    </div>
  )
}

function ActuationDetail({ item }: { item: ApprovalItem }) {
  const planId =
    item.action === 'apply_manifest' && typeof item.params.plan_id === 'string' ? item.params.plan_id : ''
  const command = commandText(item.params)
  const plan = useQuery({
    queryKey: ['actuation-plan', planId],
    enabled: planId !== '',
    queryFn: async () => {
      const response = await authedFetch(
        'Plan',
        `/api/v1/actuation/manifests/plans/${encodeURIComponent(planId)}`,
      )
      return (await response.json()) as {
        objects?: unknown
        policy?: string
        ready?: boolean
        logs?: string
      }
    },
  })
  // The command has its own block above this one.
  if (command != null) return null
  const pipelineParams =
    item.action === 'start_pipeline_run' && item.params.params != null && typeof item.params.params === 'object'
      ? (item.params.params as Record<string, unknown>)
      : null
  if (pipelineParams != null) {
    return (
      <>
        {Object.entries(pipelineParams).map(([key, value]) => (
          <Field key={key} label={key} value={value == null ? '' : String(value)} />
        ))}
      </>
    )
  }
  if (planId === '') return null
  const summary = plan.data
  const status =
    summary == null
      ? planId
      : `${planId} · ${summary.policy ?? 'pending'} · ${summary.ready ? 'ready' : 'not ready'}`
  return (
    <>
      <Field label="Plan" value={status} />
      {summary?.logs ? <Field label="Plan logs" value={summary.logs} /> : null}
      {summary?.objects != null ? (
        <pre className="m-0 max-h-48 w-full min-w-0 max-w-full overflow-auto whitespace-pre font-mono text-xs">
          {JSON.stringify(summary.objects, null, 2)}
        </pre>
      ) : null}
    </>
  )
}

function RelatedRequests({
  item,
  rows,
}: {
  item: ApprovalItem
  rows: readonly ApprovalItem[] | undefined
}) {
  if (rows == null) return null
  const siblings = siblingRequests(item, rows)
  if (siblings.length === 0) return null
  return (
    <ul aria-label="Related requests" className="m-0 flex w-full min-w-0 list-none flex-col gap-1 p-0">
      {siblings.map(other => (
        <li key={other.id} className="break-words text-sm">
          The same command was requested again as{' '}
          <a href={approvalRequestHref(other.id)} className="underline">
            {requestRef(other)}
          </a>
          {': '}
          {statusInPlainWords(other.status)}
        </li>
      ))}
    </ul>
  )
}

function WhatIsKnown({ item, rows }: { item: ApprovalItem; rows: readonly ApprovalItem[] | undefined }) {
  const claimed = claimedSentence(item.execution)
  return (
    <section aria-label="What is known" className="flex w-full min-w-0 flex-col gap-1">
      <h3 className="m-0 text-sm font-medium">What is known</h3>
      {claimed != null ? <p className="m-0 break-words text-sm">{claimed}</p> : null}
      <p className="m-0 text-sm">No result was posted.</p>
      <RelatedRequests item={item} rows={rows} />
    </section>
  )
}

function ApprovalCard({
  item,
  blocked,
  busy,
  sending,
  staleBuild,
  reloadHeld,
  actionError,
  notice,
  closeError,
  closeNotice,
  rows,
  onApprove,
  onReject,
  onClose,
}: {
  item: ApprovalItem
  blocked: string | null
  busy: boolean
  sending: 'approve' | 'reject' | null
  staleBuild: boolean
  reloadHeld: boolean
  actionError: string | null
  notice: string | null
  closeError: string | null
  closeNotice: string | null
  rows: readonly ApprovalItem[] | undefined
  onApprove: (id: string) => void
  onReject: (id: string, reason: string) => void
  onClose: (id: string, outcome: ResolveOutcome, note: string) => void
}) {
  const [reason, setReason] = useState('')
  const landing = isLandingAction(item.action)
  const op = typeof item.params?.op === 'string' ? item.params.op : ''
  const stg = item.params?.stg_release !== false
  const landingQ = useLandingMaterial(op, stg, isFinished(item), landing)
  const evidence = landingQ.displayData
  const matches = typeof item.params?.stg_release === 'boolean' && item.params.env === 'prod'
    && evidence?.material.material_digest === item.params?.material_digest && evidence?.material.candidate.R === item.params?.R
    && evidence?.material.action === item.action && evidence?.material.candidate.task === item.params?.task && evidence?.material.candidate.repo === item.params?.repo
  const [checkedVersion, setCheckedVersion] = useState('')
  const version = JSON.stringify([item.id, item.params_hash, evidence?.material.material_digest, evidence?.material.candidate.decision_chain])
  if (checkedVersion !== '' && checkedVersion !== version) setCheckedVersion('')
  const checked = checkedVersion === version
  const landingReady = !landing || (landingQ.confirmed && matches && evidence?.main_known === true && evidence.main_is_M0 && evidence.material.latest
    && evidence.material.phase === 'awaiting_grant' && evidence.material.problems.length === 0
    && Number.isSafeInteger(item.number) && item.number! > 0
    && (item.action !== 'approve_core_landing' || !evidence.material.candidate.decision_chain || checked))
  const pending = item.status === 'pending'
  const canDecide = pending && blocked == null && !busy && !staleBuild
  const waitingForReload = staleBuild && reloadHeld
  const blockedReason = staleBuild ? (waitingForReload ? DECISION_AFTER_RELOAD : NEWER_CONSOLE_BANNER) : undefined
  const buildReasonId = waitingForReload ? `console-build-reason-${item.id}` : 'console-build-reason'
  const trimmed = reason.trim()
  const title = decisionTitle(item)
  const env = decisionEnv(item)
  const words = landing && !item.drill ? 'approval authorizes this candidate; the landing gate checks again before push' : tierWords(item.tier, item.drill === true)
  const { lead, rest } = splitReason(item.reason)
  const chips = workItems(item.reason)
  const command = commandText(item.params)
  const paramsLeft = technicalParams(item.params)
  const terminal = ownerTerminal(item)
  const fromTradeInfra = runFromTradeInfra(terminal)
  const outcome = executionOutcome(item)
  const unknown = unknownFollowup(item)
  const closed = closedByYou(item.resolution)
  const outputTail = executionOutputTail(item.execution)
  const attempts = executionAttempts(item.execution)
  const executor = executionExecutor(item.execution)
  const resultText = item.error ? null : technicalResult(item.result, item.params)
  const created = visibleTimestamp(item.created_at)
  const expires = visibleTimestamp(item.expires_at)
  const decided = visibleTimestamp(item.decided_at)
  return (
    <div className="flex w-full min-w-0 max-w-full flex-col gap-3">
      <article
        id={`approval-${item.id}`}
        data-open="true"
        aria-current="true"
        className="ops-decision-card flex w-full min-w-0 max-w-full flex-col gap-3 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-3"
      >
        <h2 className="m-0 break-words text-base font-medium" data-approval-ref>
          {title}
        </h2>
        {landing && <LandingApprovalEvidence item={item} data={evidence} error={landingQ.isError && !landingQ.waiting || landingQ.isPaused || (!!evidence && !matches) || op === ''} checked={checked} onCheck={value => setCheckedVersion(value ? version : '')} />}
        {landing && landingQ.waiting && <p role="status">{landingQ.waitingMessage} Approval waits for current material and main confirmation.</p>}
        <div className="flex min-w-0 max-w-full flex-wrap items-center gap-2">
          {env !== '' ? <DenseTag variant="neutral">{env}</DenseTag> : null}
          <DenseTag variant={item.tier === 'D' ? 'danger' : item.tier === 'B' ? 'neutral' : 'warning'}>
            {item.tier || '—'}
          </DenseTag>
          {words !== '' ? <span className="break-words text-sm">{words}</span> : null}
          <DenseTag variant="neutral">{item.status}</DenseTag>
        </div>
        <p
          role="status"
          data-decision-banner
          className="m-0 max-w-full break-words rounded-[var(--control-radius)] border border-[var(--card-border)] bg-[var(--field-fill)] px-3 py-2 text-sm"
        >
          {decisionBanner(item)}
        </p>
        {actionError != null ? (
          <p role="alert" className="m-0 text-sm text-destructive">
            {actionError}
          </p>
        ) : null}
        {closed != null ? (
          <p data-closed-by-you className="m-0 max-w-full break-words text-sm">
            {closed}
          </p>
        ) : null}
        {outcome != null ? (
          <p data-execution-outcome className="m-0 max-w-full break-words text-sm">
            {outcome}
          </p>
        ) : null}
        {unknown.knows != null ? (
          <p data-platform-knows className="m-0 max-w-full break-words text-sm">
            {unknown.knows}
          </p>
        ) : null}
        {item.status === 'unknown' ? (
          <WhatIsKnown item={item} rows={rows} />
        ) : (
          <RelatedRequests item={item} rows={rows} />
        )}
        <ApprovalPlanSteps number={item.number} />
        {item.status === 'unknown' ? (
          <CloseUnknownForm
            blocked={blocked}
            busy={busy}
            error={closeError}
            notice={closeNotice}
            onClose={(outcome, note) => onClose(item.id, outcome, note)}
          />
        ) : null}
        <Prose label="If you approve" value={lead} />
        {rest !== '' ? <Prose label="Why" value={rest} /> : null}
        <Prose label="If it goes wrong" value={item.rollback ?? ''} />
        {/* The request's AI assessment (TD-391): advice beside it; only the buttons below decide. */}
        {pending && item.number != null && item.number > 0 ? (
          <AssessmentSection kind="approval" id={String(item.number)} ruleKey={null} label={approvalOptionLabel} />
        ) : null}
        <div className="min-w-0 max-w-full">
          <div className="text-xs uppercase tracking-wide text-muted-foreground">Asked by</div>
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <span className="break-words">{item.requester || '—'}</span>
            {chips.map(id => (
              <DenseTag key={id} variant="neutral">
                {id}
              </DenseTag>
            ))}
          </div>
        </div>
        {terminal != null && item.status !== 'unknown' ? (
          <div className="min-w-0 max-w-full">
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              After approving, run in a terminal:
            </div>
            <div className="mt-1">
              <CodeBox text={terminal.line} marker="owner" />
            </div>
            {terminal.fallback ? (
              <p className="m-0 mt-1 break-words text-sm">run the owner-run script with this id</p>
            ) : null}
            {fromTradeInfra ? (
              <p data-run-folder className="m-0 mt-1 break-words text-sm">
                Run it from the bifrost-trade-infra folder.
              </p>
            ) : null}
          </div>
        ) : null}
        {/* Last in the card, so it sits directly above the decision buttons. */}
        {pending && notice != null ? (
          <p role="status" className="m-0 text-sm">
            {notice}
          </p>
        ) : null}
      </article>
      {pending ? (
        <div className="ops-decision-actions flex w-full min-w-0 flex-col gap-2">
          <p className="ops-decision-context">{env || 'Environment not specified'} · {title}</p>
          <ServerContactStatus />
          {waitingForReload ? (
            <p id={buildReasonId} role="status" className="m-0 text-sm">
              {blockedReason}
            </p>
          ) : null}
          {blocked != null ? (
            <p role="status" className="m-0 text-sm">
              {blocked}
            </p>
          ) : null}
          <label className="flex min-w-0 flex-col gap-1 text-sm" htmlFor={`reject-${item.id}`}>
            Reject reason
            <input
              id={`reject-${item.id}`}
              className="min-h-11 w-full min-w-0 rounded-[var(--control-radius)] border border-transparent bg-[var(--field-fill)] px-2 py-1 text-base outline-none focus-visible:ring-3 focus-visible:ring-[var(--focus-glow)]"
              value={reason}
              onChange={event => setReason(event.target.value)}
            />
          </label>
          <div className="flex w-full min-w-0 flex-col gap-2 sm:flex-row">
            {/* A drill has no approval to give (v4 §20.5 scenario 9): no button for one; reject closes it. */}
            {item.drill === true ? null : (
              <ApprovalReviewButton
                item={item}
                type="button"
                className="h-11 w-full sm:w-auto"
                disabled={!canDecide || !landingReady}
                title={blockedReason}
                aria-describedby={staleBuild ? buildReasonId : undefined}
                aria-label={sending === 'approve' ? 'Sending…' : `Approve ${item.action}`}
                onClick={() => onApprove(item.id)}
              >
                {sending === 'approve' ? 'Sending…' : 'Approve'}
              </ApprovalReviewButton>
            )}
            <Button
              type="button"
              variant="outline"
              className="h-11 w-full sm:w-auto"
              disabled={!canDecide || trimmed === ''}
              title={blockedReason}
              aria-describedby={staleBuild ? buildReasonId : undefined}
              aria-label={sending === 'reject' ? 'Sending…' : `Reject ${item.action}`}
              onClick={() => onReject(item.id, trimmed)}
            >
              {sending === 'reject' ? 'Sending…' : 'Reject'}
            </Button>
          </div>
        </div>
      ) : null}
      <details className="min-w-0 max-w-full">
        <summary className="cursor-pointer text-sm pointer-coarse:py-3">Technical details</summary>
        <div className="mt-2 flex w-full min-w-0 max-w-full flex-col gap-3">
          {command != null ? (
            <div className="min-w-0 max-w-full">
              <div className="text-xs uppercase tracking-wide text-muted-foreground">Command</div>
              <div className="mt-1">
                <CodeBox text={command} capped marker="command" />
              </div>
            </div>
          ) : null}
          {paramsLeft != null ? (
            <div className="min-w-0 max-w-full">
              <div className="text-xs uppercase tracking-wide text-muted-foreground">Params</div>
              <pre
                data-approval-params
                className="m-0 mt-1 max-h-48 w-full min-w-0 max-w-full overflow-auto whitespace-pre font-mono text-xs"
              >
                {paramsLeft}
              </pre>
            </div>
          ) : null}
          {item.error ? <Field label="Error" value={item.error} /> : null}
          {resultText != null ? (
            <div className="min-w-0 max-w-full">
              <div className="text-xs uppercase tracking-wide text-muted-foreground">Result</div>
              <pre
                data-approval-result
                className="m-0 mt-1 max-h-48 w-full min-w-0 max-w-full overflow-auto whitespace-pre-wrap break-words font-mono text-xs"
              >
                {resultText}
              </pre>
            </div>
          ) : null}
          {outputTail != null ? (
            <div className="min-w-0 max-w-full">
              <div className="text-xs uppercase tracking-wide text-muted-foreground">Output (last lines)</div>
              <pre
                data-output-tail
                className="m-0 mt-1 max-h-48 w-full min-w-0 max-w-full overflow-auto whitespace-pre font-mono text-xs"
              >
                {outputTail}
              </pre>
            </div>
          ) : null}
          {attempts != null ? <Field label="Attempts" value={String(attempts)} /> : null}
          {executor != null ? <Field label="Executor" value={executor} /> : null}
          {item.reject_reason ? <Field label="Reject reason" value={item.reject_reason} /> : null}
          <Field label="Action" value={item.action} />
          <Field label="Request id" value={item.id} />
          {item.params_hash ? <Field label="Params hash" value={item.params_hash} /> : null}
          {item.approval_line ? <Field label="Approval line" value={item.approval_line} /> : null}
          {created != null ? <Field label="Created" value={created} /> : null}
          {expires != null ? <Field label="Expires" value={expires} /> : null}
          {decided != null ? <Field label="Decided at" value={decided} /> : null}
          {pending && !isUnsetDecidedAt(item.created_at) ? (
            <Field
              label="Waited · time remaining"
              value={`${formatWaited(item.created_at)} · ${formatTimeRemaining(item.expires_at)}`}
            />
          ) : null}
          <ActuationDetail item={item} />
        </div>
      </details>
    </div>
  )
}

/** The request page, `#approvals?id=<id>`: the only place to approve or reject. */
export function ApprovalsPage() {
  useDecisionPageResume()
  const qc = useQueryClient()
  const auth = usePlatformAuth()
  const blocked = decisionBlockReason(auth)
  const id = useDeepLinkId()
  const recordId = qc.getQueryData<ApprovalItem>(['approvals', 'item', id])?.id ?? id
  const [busy, setBusy] = useRecordState(recordId, false)
  const [sending, setSending] = useRecordState<'approve' | 'reject' | null>(recordId, null)
  const locks = useRef(new Map<string | null, boolean>())
  const [checkingId, setCheckingId] = useRecordState<string | null>(recordId, null)
  const [actionError, setActionError] = useRecordState<string | null>(recordId, null)
  const [notice, setNotice] = useRecordState<string | null>(recordId, null)
  const [closeError, setCloseError] = useRecordState<string | null>(recordId, null)
  const [closeNotice, setCloseNotice] = useRecordState<string | null>(recordId, null)
  const staleBuild = useConsoleBuildDrift()
  const latest = useRef({ auth, staleBuild })
  latest.current = { auth, staleBuild }
  const postsInFlight = useRef(0)
  const [reloadHeld, setReloadHeld] = useState(false)

  useEffect(() => {
    clearLegacyApprovalToken()
  }, [])

  const itemQ = useQuery({
    queryKey: ['approvals', 'item', id],
    queryFn: () => fetchApproval(id ?? ''),
    structuralSharing: keepDecision,
    enabled: id != null,
    retry: false,
    refetchInterval: query => {
      if (busy) return false
      const data = query.state.data
      if (data == null || !isFinished(data)) return NEEDS_YOU_REFRESH_MS
      return false
    },
  })
  const opened = id == null ? null : (itemQ.data ?? null)
  const allQ = useQuery({
    queryKey: ['approvals', 'all'],
    queryFn: () => fetchApprovalList('all'),
    enabled: id != null,
    retry: false,
  })

  function storeRecord(readId: string | null, fresh: ApprovalItem) {
    qc.setQueryData(['approvals', 'item', readId], old => keepDecision(old, fresh))
    // Already-opened aliases belong to the same record, but keep their read keys.
    qc.setQueriesData<ApprovalItem>({ queryKey: ['approvals', 'item'] }, old =>
      old?.id === fresh.id ? keepDecision(old, fresh) : old)
  }

  useEffect(() => {
    if (checkingId == null) return
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    const check = async () => {
      try {
        const readId = id ?? checkingId
        const fresh = await fetchApproval(readId)
        if (cancelled) return
        storeRecord(readId, fresh)
        const confirmed = qc.getQueryData<ApprovalItem>(['approvals', 'item', readId])
        setNotice(confirmed?.status === 'pending'
          ? 'The request is still pending. That attempt did not take effect. Try again.'
          : null)
        setCheckingId(null)
        locks.current.set(recordId, false)
        setBusy(false)
      } catch {
        if (!cancelled) {
          setNotice(`Could not read this request. Last attempt: ${new Date().toLocaleString()}. Refresh this page to check again.`)
          timer = setTimeout(() => { void check() }, NEEDS_YOU_REFRESH_MS)
        }
      }
    }
    void check()
    return () => { cancelled = true; clearTimeout(timer) }
  }, [checkingId, qc, recordId, id])

  // The connection check takes up to five seconds: decide again on what is true when it ends.
  function sendWhileAllowed<T>(session: string, send: () => Promise<T>, guardBuild = true): Promise<T> {
    const now = latest.current
    if (decisionBlockReason(now.auth) != null || now.auth.token !== session || (guardBuild && now.staleBuild)) return Promise.reject(new ConnectionCheckError(DECISION_CHANGED))
    return send()
  }
  async function decide(requestId: string, path: 'approve' | 'reject', reason?: string) {
    if (decisionBlockReason(auth) != null || locks.current.get(recordId)) return
    if (latest.current.staleBuild) {
      // Say what the banner says: with no decision in flight the page can be reloaded now.
      setNotice(postsInFlight.current > 0 ? DECISION_AFTER_RELOAD : NEWER_CONSOLE_BANNER)
      return
    }
    const session = auth.token
    locks.current.set(recordId, true)
    postsInFlight.current += 1
    setReloadHeld(true)
    setSending(path)
    setBusy(true)
    setActionError(null)
    setNotice(null)
    let needsCheck = false
    try {
      const decision = await withDecisionContact(() => sendWhileAllowed(session, () => postApprovalDecision(
        requestId,
        session,
        path,
        path === 'approve'
          ? {
              channel: 'console',
              approval_line: opened?.approval_line ?? '',
              params_hash: opened?.params_hash ?? '',
              ...(isLandingAction(opened?.action) ? { confirm_number: opened?.number } : {}),
            }
          : { reason: reason ?? '' },
      )), setNotice)
      if (decision.status != null && decision.status !== 'pending' && opened != null) {
        storeRecord(id, { ...opened, ...decision })
        void qc.invalidateQueries({ queryKey: ['approvals'] })
      } else {
        needsCheck = true
        setNotice('Checking the decision result.')
        setCheckingId(id)
      }
    } catch (err) {
      if (err instanceof ConnectionCheckError) { setNotice(err.message); return }
      needsCheck = true
      const status = approvalFailureStatus(err)
      if (status === 401 || status === 403) {
        setActionError(DECISION_REJECTED)
        auth.refreshCapabilities()
      } else {
        const raw = err instanceof Error ? err.message : 'Approval action failed'
        setActionError(scrubSecret(raw, session))
      }
      setNotice('Checking the decision result.')
      setCheckingId(id)
    } finally {
      postsInFlight.current = Math.max(0, postsInFlight.current - 1)
      setReloadHeld(postsInFlight.current > 0)
      setSending(null)
      // Reconciliation owns the lock until a record has actually been read.
      // State updates are asynchronous; the synchronous ref covers the same turn.
      if (!needsCheck) {
        locks.current.set(recordId, false)
        setBusy(false)
      }
    }
  }

  async function closeUnknown(requestId: string, outcome: ResolveOutcome, note: string) {
    // Closing an unknown request is not held back by a newer Console: only approve and reject are.
    if (decisionBlockReason(auth) != null || locks.current.get(recordId)) return
    const session = auth.token
    locks.current.set(recordId, true)
    setBusy(true)
    setCloseError(null)
    setCloseNotice(null)
    try {
      await withDecisionContact(() => sendWhileAllowed(session, () => postApprovalResolve(requestId, session, { outcome, note }), false), setCloseNotice)
      await qc.invalidateQueries({ queryKey: ['approvals'] })
    } catch (err) {
      if (err instanceof ConnectionCheckError) { setCloseNotice(err.message); return }
      const status = approvalFailureStatus(err)
      if (status === 401 || status === 403) {
        setCloseError(DECISION_REJECTED)
        auth.refreshCapabilities()
      } else {
        const raw = err instanceof Error ? err.message : 'Could not close this request'
        setCloseError(scrubSecret(raw, session))
      }
    } finally {
      locks.current.set(recordId, false)
      setBusy(false)
    }
  }

  return (
    <div data-testid="approvals-page" className="ops-decision-page flex w-full min-w-0 max-w-3xl flex-col gap-4 overflow-x-clip">
      <ConsoleBuildBanner stale={staleBuild} holdReload={reloadHeld} />
      <a href={NEEDS_YOU_HREF} className="text-sm text-muted-foreground no-underline hover:text-foreground pointer-coarse:py-3">
        ← For you
      </a>
      {id == null ? (
        <p className="m-0 text-sm text-muted-foreground">No request named in the link.</p>
      ) : (
        <section aria-label={`Approval ${id}`} className="flex w-full min-w-0 flex-col gap-2">
          {opened == null && blocked != null ? <p className="m-0 text-sm">{blocked}</p> : null}
          {itemQ.isPaused ? <p role="alert">Could not read approvals — offline.</p> : null}
          {itemQ.isLoading && !itemQ.isPaused ? <p className="m-0 text-sm">Opening {id}…</p> : null}
          {itemQ.isError ? (
            <p role="alert" className="m-0 text-sm text-destructive">
              {approvalsReadFailure(itemQ.errorUpdatedAt)}
            </p>
          ) : null}
          {opened != null ? (
            <ApprovalCard
              key={opened.id}
              item={opened}
              blocked={blocked}
              busy={busy}
              sending={sending}
              staleBuild={staleBuild && !(reloadHeld && sending != null)}
              reloadHeld={reloadHeld}
              actionError={actionError}
              notice={notice}
              closeError={closeError}
              closeNotice={closeNotice}
              rows={allQ.data}
              onApprove={requestId => void decide(requestId, 'approve')}
              onReject={(requestId, reason) => void decide(requestId, 'reject', reason)}
              onClose={(requestId, outcome, note) => void closeUnknown(requestId, outcome, note)}
            />
          ) : null}
        </section>
      )}
    </div>
  )
}
