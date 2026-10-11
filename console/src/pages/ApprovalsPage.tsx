import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Button, DenseTag } from '@bifrost/ui'
import { authedFetch } from '@/api/client'
import {
  approvalIdFromHash,
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
import { useDecisionAuth } from '@/hooks/useDecisionAuth'
import { adminSessionToken, isAdminRoleRefusal, isDecisionNotSent, type DecisionGate } from '@/lib/decisionAuth'
import { DecisionHold } from '@/pages/DecisionHold'
import { NEEDS_YOU_REFRESH_MS } from '@/pages/shell/needs-you/useNeedsYou'

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

const CLOSE_CHOICES: readonly { outcome: ResolveOutcome; label: string }[] = [
  { outcome: 'not_run', label: 'It did not run' },
  { outcome: 'succeeded', label: 'It ran and succeeded' },
  { outcome: 'failed', label: 'It ran and failed' },
]

function CloseUnknownForm({
  maySend,
  busy,
  error,
  onClose,
}: {
  maySend: boolean
  busy: boolean
  error: string | null
  onClose: (outcome: ResolveOutcome, note: string) => void
}) {
  const [outcome, setOutcome] = useState<ResolveOutcome | ''>('')
  const [note, setNote] = useState('')
  const trimmed = note.trim()
  const ready = maySend && !busy && outcome !== '' && trimmed !== ''
  return (
    <form
      className="flex w-full min-w-0 flex-col gap-2"
      onSubmit={event => {
        event.preventDefault()
        if (outcome === '' || trimmed === '') return
        onClose(outcome, trimmed)
      }}
    >
      <fieldset className="m-0 flex min-w-0 flex-col gap-1 border-0 p-0">
        <legend className="text-sm">What happened</legend>
        {CLOSE_CHOICES.map(choice => (
          <label key={choice.outcome} className="flex items-center gap-2 text-sm">
            <input
              type="radio"
              name="outcome"
              value={choice.outcome}
              checked={outcome === choice.outcome}
              onChange={() => setOutcome(choice.outcome)}
            />
            {choice.label}
          </label>
        ))}
      </fieldset>
      <p className="m-0 text-sm">If the same command later ran as another request, this one did not run.</p>
      <label className="flex min-w-0 flex-col gap-1 text-sm" htmlFor="close-note">
        Note
        <input
          id="close-note"
          required
          value={note}
          onChange={event => setNote(event.target.value)}
          className="w-full min-w-0 rounded-[var(--control-radius)] border border-transparent bg-[var(--field-fill)] px-2 py-1 outline-none focus-visible:ring-3 focus-visible:ring-[var(--focus-glow)]"
        />
      </label>
      <Button type="submit" className="h-11 w-full sm:h-auto sm:w-auto" disabled={!ready}>
        Close this request
      </Button>
      {error != null ? (
        <p role="alert" className="m-0 text-sm text-destructive">
          {error}
        </p>
      ) : null}
    </form>
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
  maySend,
  gate,
  refusal,
  busy,
  closeError,
  rows,
  onApprove,
  onReject,
  onClose,
}: {
  item: ApprovalItem
  maySend: boolean
  gate: DecisionGate
  refusal: string | null
  busy: boolean
  closeError: string | null
  rows: readonly ApprovalItem[] | undefined
  onApprove: (id: string) => void
  onReject: (id: string, reason: string) => void
  onClose: (id: string, outcome: ResolveOutcome, note: string) => void
}) {
  const [reason, setReason] = useState('')
  const pending = item.status === 'pending'
  const canDecide = pending && maySend && !busy
  const trimmed = reason.trim()
  const title = decisionTitle(item)
  const env = decisionEnv(item)
  const words = tierWords(item.tier)
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
        className="flex w-full min-w-0 max-w-full flex-col gap-3 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-3"
      >
        <h2 className="m-0 break-words text-base font-medium" data-approval-ref>
          {title}
        </h2>
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
        {item.status === 'unknown' ? (
          <>
            <DecisionHold gate={gate} refusal={refusal} />
            <CloseUnknownForm
              maySend={maySend}
              busy={busy}
              error={closeError != null && isAdminRoleRefusal(closeError) ? null : closeError}
              onClose={(outcome, note) => onClose(item.id, outcome, note)}
            />
          </>
        ) : null}
        <Prose label="If you approve" value={lead} />
        {rest !== '' ? <Prose label="Why" value={rest} /> : null}
        <Prose label="If it goes wrong" value={item.rollback ?? ''} />
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
      </article>
      {pending ? (
        <div className="flex w-full min-w-0 flex-col gap-2">
          <DecisionHold gate={gate} refusal={refusal} />
          <label className="flex min-w-0 flex-col gap-1 text-sm" htmlFor={`reject-${item.id}`}>
            Reject reason
            <input
              id={`reject-${item.id}`}
              className="w-full min-w-0 rounded-[var(--control-radius)] border border-transparent bg-[var(--field-fill)] px-2 py-1 outline-none focus-visible:ring-3 focus-visible:ring-[var(--focus-glow)]"
              value={reason}
              onChange={event => setReason(event.target.value)}
            />
          </label>
          <div className="flex w-full min-w-0 flex-col gap-2 sm:flex-row">
            <Button
              type="button"
              className="h-11 w-full sm:h-auto sm:w-auto"
              disabled={!canDecide}
              aria-label={`Approve ${item.action}`}
              onClick={() => onApprove(item.id)}
            >
              Approve
            </Button>
            <Button
              type="button"
              variant="outline"
              className="h-11 w-full sm:h-auto sm:w-auto"
              disabled={!canDecide || trimmed === ''}
              aria-label={`Reject ${item.action}`}
              onClick={() => onReject(item.id, trimmed)}
            >
              Reject
            </Button>
          </div>
        </div>
      ) : null}
      <details className="min-w-0 max-w-full">
        <summary className="cursor-pointer text-sm">Technical details</summary>
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
  const qc = useQueryClient()
  const id = useDeepLinkId()
  const gate = useDecisionAuth()
  const maySend = gate.kind === 'admin'
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [closeError, setCloseError] = useState<string | null>(null)
  const refusal = [actionError, closeError].find(message => message != null && isAdminRoleRefusal(message)) ?? null

  const itemQ = useQuery({
    queryKey: ['approvals', 'item', id],
    queryFn: () => fetchApproval(id ?? ''),
    enabled: id != null,
    retry: false,
    refetchInterval: query => (query.state.data?.status === 'pending' ? NEEDS_YOU_REFRESH_MS : false),
  })
  const opened = id == null ? null : (itemQ.data ?? null)
  const allQ = useQuery({
    queryKey: ['approvals', 'all'],
    queryFn: () => fetchApprovalList('all'),
    enabled: id != null,
    retry: false,
  })

  async function decide(requestId: string, path: 'approve' | 'reject', reason?: string) {
    if (adminSessionToken() == null) return
    setBusy(true)
    setActionError(null)
    try {
      await postApprovalDecision(
        requestId,
        path,
        path === 'approve'
          ? {
              channel: 'console',
              approval_line: opened?.approval_line ?? '',
              params_hash: opened?.params_hash ?? '',
            }
          : { reason: reason ?? '' },
      )
      await qc.invalidateQueries({ queryKey: ['approvals'] })
    } catch (err) {
      if (!isDecisionNotSent(err)) {
        setActionError(err instanceof Error ? err.message : 'Approval action failed')
      }
    } finally {
      setBusy(false)
    }
  }

  async function closeUnknown(requestId: string, outcome: ResolveOutcome, note: string) {
    if (adminSessionToken() == null) return
    setBusy(true)
    setCloseError(null)
    try {
      await postApprovalResolve(requestId, { outcome, note })
      await qc.invalidateQueries({ queryKey: ['approvals'] })
    } catch (err) {
      if (!isDecisionNotSent(err)) {
        setCloseError(err instanceof Error ? err.message : 'Could not close this request')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <div data-testid="approvals-page" className="flex w-full min-w-0 max-w-3xl flex-col gap-4 overflow-x-clip">
      <a href="#needs-you" className="text-sm text-muted-foreground no-underline hover:text-foreground">
        ← Needs you
      </a>
      {actionError != null && !isAdminRoleRefusal(actionError) ? (
        <p className="m-0 text-sm text-destructive">{actionError}</p>
      ) : null}

      {id == null ? (
        <p className="m-0 text-sm text-muted-foreground">No request named in the link.</p>
      ) : (
        <section aria-label={`Approval ${id}`} className="flex w-full min-w-0 flex-col gap-2">
          {itemQ.isLoading ? <p className="m-0 text-sm">Opening {id}…</p> : null}
          {itemQ.error != null ? (
            <p className="m-0 text-sm text-destructive">
              {itemQ.error instanceof Error ? itemQ.error.message : 'Could not open this request'}
            </p>
          ) : null}
          {opened != null ? (
            <ApprovalCard
              key={opened.id}
              item={opened}
              maySend={maySend}
              gate={gate}
              refusal={refusal}
              busy={busy}
              closeError={closeError}
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
