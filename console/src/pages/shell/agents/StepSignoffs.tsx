import { DecisionEnvironment } from '@/pages/DecisionEnvironment'
import { useDecisionEnvironment } from '@/pages/useDecisionEnvironment'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { postStepSignoff, unsignedRefs, type SignoffAnswer, type StepSignoffs } from '@/api/agentRuntimePlan'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { decisionBlockReason, scrubSecret } from '@/pages/decisionSignIn'
import { SIGNOFF_PRESS_AGAIN, signedLine } from './stepSignoffText'

/** States the platform refuses a sign-off in: it would move nothing. */
const CLOSED = new Set(['done', 'stopped', 'dropped'])

type Phase =
  | { kind: 'idle' }
  | { kind: 'confirm'; identity: string | null }
  | { kind: 'sending' }
  | { kind: 'signed'; answer: SignoffAnswer }
  | { kind: 'failed'; error: string }

/**
 * One owner_signoff of a step: the Sign off button, usable only while the Owner is signed in (the admin role, as for
 * deciding a request); one confirmation, then POST /plan/{id}/signoff. What the platform recorded is shown at once;
 * the plan is read again, and the step moves on the runtime's next rules pass.
 */
function SignOffButton({ node, label, signoffRef, named, reviewContext }: { node: string; label: string; signoffRef: string; named: boolean; reviewContext: string }) {
  const auth = usePlatformAuth()
  const blocked = decisionBlockReason(auth)
  const qc = useQueryClient()
  const environment = useDecisionEnvironment()
  const identity = JSON.stringify([auth.token, auth.caps?.role, blocked, environment, node, label, signoffRef, reviewContext])
  const [phase, setPhase] = useState<Phase>({ kind: 'idle' })
  // Latch invalidation while reviewing, without touching an in-flight or recorded sign-off.
  if (phase.kind === 'confirm' && phase.identity !== null && phase.identity !== identity) {
    setPhase({ kind: 'confirm', identity: null })
  }
  if (phase.kind === 'signed') {
    return <p role="status" className="ops-decision-receipt m-0 break-words text-xs text-muted-foreground" data-signed-off={signoffRef}>
      {signedLine(phase.answer)}{phase.answer.alreadySigned ? ' (recorded before this press)' : ''}
    </p>
  }
  const send = async () => {
    if (decisionBlockReason(auth) !== null || phase.kind !== 'confirm' || phase.identity !== identity) return
    setPhase({ kind: 'sending' })
    try {
      const answer = await postStepSignoff(node, signoffRef, auth.token)
      setPhase({ kind: 'signed', answer })
      // Every round's tree: a step of any round may be signed here, and the waits list reads them all.
      void qc.invalidateQueries({ queryKey: ['agent-runtime-plan'] })
      void qc.invalidateQueries({ queryKey: ['agent-runtime-plan-view'] })
      void qc.invalidateQueries({ queryKey: ['agent-runtime-progress'] })
    } catch (e) {
      setPhase({ kind: 'failed', error: scrubSecret(e instanceof Error ? e.message : String(e), auth.token) })
    }
  }
  const what = named ? `Sign off ${signoffRef}` : 'Sign off'
  return <div className="ops-signoff-action flex w-full min-w-0 flex-col gap-1" data-signoff-ref={signoffRef}>
    {phase.kind === 'confirm' || phase.kind === 'sending' ? <div className="ops-decision-actions">
      <DecisionEnvironment />
      {phase.kind === 'confirm' && phase.identity !== identity ? <p role="status">The sign-in or step changed. Cancel and review this sign-off again.</p> : null}
      <p className="ops-decision-context">{node} · {signoffRef}</p>
      <p className="m-0 break-words text-sm">Sign off step {label} ({signoffRef}) as the Owner? It is recorded under your name; the step is done
        on the runtime's next pass when nothing else in its rule is waited on.</p>
      <div className="flex flex-wrap gap-2">
        <button type="button" style={{ minHeight: 44 }} className="rounded-[var(--card-radius)] border border-foreground bg-foreground px-3 text-sm font-semibold text-background"
          disabled={phase.kind === 'sending' || blocked !== null || (phase.kind === 'confirm' && phase.identity !== identity)} onClick={() => { void send() }}>
          {phase.kind === 'sending' ? 'Signing off…' : 'Confirm sign-off'}
        </button>
        <button type="button" style={{ minHeight: 44 }} className="rounded-[var(--card-radius)] border border-[var(--card-border)] px-3 text-sm"
          disabled={phase.kind === 'sending'} onClick={() => setPhase({ kind: 'idle' })}>Cancel</button>
      </div>
    </div> : <button type="button" style={{ minHeight: 44 }} className="self-start rounded-[var(--card-radius)] border border-[var(--card-border)] px-3 text-sm"
      disabled={blocked !== null} onClick={() => setPhase({ kind: 'confirm', identity })}>{what}</button>}
    {blocked !== null ? <p className="m-0 break-words text-xs text-muted-foreground">{blocked}</p> : null}
    {phase.kind === 'failed' ? <p role="alert" className="m-0 break-words text-xs text-destructive">{phase.error} {SIGNOFF_PRESS_AGAIN}</p> : null}
  </div>
}

/**
 * Under a step whose done rule waits on the Owner's sign-off: each sign-off it holds (who, when), and a Sign off button
 * for each ref not signed yet while the step can still move (not done, stopped or dropped).
 */
/** The Sign off buttons of a step's refs not signed yet; each one named by its ref when the rule waits on more than one. */
export function SignOffButtons({ node, label, refs, named, state = '', title = '' }: { node: string; label: string; refs: readonly string[]; named: boolean; state?: string; title?: string }) {
  return <div className="flex min-w-0 flex-col gap-1">{refs.map(ref => <SignOffButton key={ref} node={node} label={label} signoffRef={ref} named={named} reviewContext={JSON.stringify([state, title, refs])} />)}</div>
}

export function StepSignoffControls({ label, state, signoffs }: { label: string; state: string; signoffs: StepSignoffs }) {
  const open = CLOSED.has(state) ? [] : unsignedRefs(signoffs)
  return <div data-step-signoffs={signoffs.node} className="mt-1 flex w-full min-w-0 flex-col gap-1">
    {signoffs.signed.map(s => <p key={`${s.ref}-${s.at}`} className="ops-decision-receipt m-0 break-words text-xs text-muted-foreground" data-signed-off={s.ref}>{signedLine(s)}</p>)}
    {open.map(ref => <SignOffButton key={ref} node={signoffs.node} label={label} signoffRef={ref} named={signoffs.refs.length > 1} reviewContext={JSON.stringify([state, signoffs])} />)}
  </div>
}
