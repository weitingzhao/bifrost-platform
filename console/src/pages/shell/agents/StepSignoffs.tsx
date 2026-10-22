import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { PLAN_ROOT, postStepSignoff, unsignedRefs, type SignoffAnswer, type StepSignoffs } from '@/api/agentRuntimePlan'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { decisionBlockReason, scrubSecret } from '@/pages/decisionSignIn'
import { SIGNOFF_PRESS_AGAIN, signedLine } from './stepSignoffText'

/** States the platform refuses a sign-off in: it would move nothing. */
const CLOSED = new Set(['done', 'stopped', 'dropped'])

type Phase =
  | { kind: 'idle' }
  | { kind: 'confirm' }
  | { kind: 'sending' }
  | { kind: 'signed'; answer: SignoffAnswer }
  | { kind: 'failed'; error: string }

/**
 * One owner_signoff of a step: the Sign off button, usable only while the Owner is signed in (the admin role, as for
 * deciding a request); one confirmation, then POST /plan/{id}/signoff. What the platform recorded is shown at once;
 * the plan is read again, and the step moves on the runtime's next rules pass.
 */
function SignOffButton({ node, label, signoffRef, named }: { node: string; label: string; signoffRef: string; named: boolean }) {
  const auth = usePlatformAuth()
  const blocked = decisionBlockReason(auth)
  const qc = useQueryClient()
  const [phase, setPhase] = useState<Phase>({ kind: 'idle' })
  if (phase.kind === 'signed') {
    return <p role="status" className="m-0 break-words text-xs text-muted-foreground" data-signed-off={signoffRef}>
      {signedLine(phase.answer)}{phase.answer.alreadySigned ? ' (recorded before this press)' : ''}
    </p>
  }
  const send = async () => {
    if (decisionBlockReason(auth) !== null) return
    setPhase({ kind: 'sending' })
    try {
      const answer = await postStepSignoff(node, signoffRef, auth.token)
      setPhase({ kind: 'signed', answer })
      void qc.invalidateQueries({ queryKey: ['agent-runtime-plan', PLAN_ROOT] })
      void qc.invalidateQueries({ queryKey: ['agent-runtime-progress'] })
    } catch (e) {
      setPhase({ kind: 'failed', error: scrubSecret(e instanceof Error ? e.message : String(e), auth.token) })
    }
  }
  const what = named ? `Sign off ${signoffRef}` : 'Sign off'
  return <div className="flex w-full min-w-0 flex-col gap-1" data-signoff-ref={signoffRef}>
    {phase.kind === 'confirm' || phase.kind === 'sending' ? <>
      <p className="m-0 break-words text-sm">Sign off step {label} ({signoffRef}) as the Owner? It is recorded under your name; the step is done
        on the runtime's next pass when nothing else in its rule is waited on.</p>
      <div className="flex flex-wrap gap-2">
        <button type="button" style={{ minHeight: 44 }} className="rounded-[var(--card-radius)] border border-foreground bg-foreground px-3 text-sm font-semibold text-background"
          disabled={phase.kind === 'sending' || blocked !== null} onClick={() => { void send() }}>
          {phase.kind === 'sending' ? 'Signing off…' : 'Confirm sign-off'}
        </button>
        <button type="button" style={{ minHeight: 44 }} className="rounded-[var(--card-radius)] border border-[var(--card-border)] px-3 text-sm"
          disabled={phase.kind === 'sending'} onClick={() => setPhase({ kind: 'idle' })}>Cancel</button>
      </div>
    </> : <button type="button" style={{ minHeight: 44 }} className="self-start rounded-[var(--card-radius)] border border-[var(--card-border)] px-3 text-sm"
      disabled={blocked !== null} onClick={() => setPhase({ kind: 'confirm' })}>{what}</button>}
    {blocked !== null ? <p className="m-0 break-words text-xs text-muted-foreground">{blocked}</p> : null}
    {phase.kind === 'failed' ? <p role="alert" className="m-0 break-words text-xs text-destructive">{phase.error} {SIGNOFF_PRESS_AGAIN}</p> : null}
  </div>
}

/**
 * Under a step whose done rule waits on the Owner's sign-off: each sign-off it holds (who, when), and a Sign off button
 * for each ref not signed yet while the step can still move (not done, stopped or dropped).
 */
/** The Sign off buttons of a step's refs not signed yet; each one named by its ref when the rule waits on more than one. */
export function SignOffButtons({ node, label, refs, named }: { node: string; label: string; refs: readonly string[]; named: boolean }) {
  return <div className="flex min-w-0 flex-col gap-1">{refs.map(ref => <SignOffButton key={ref} node={node} label={label} signoffRef={ref} named={named} />)}</div>
}

export function StepSignoffControls({ label, state, signoffs }: { label: string; state: string; signoffs: StepSignoffs }) {
  const open = CLOSED.has(state) ? [] : unsignedRefs(signoffs)
  return <div data-step-signoffs={signoffs.node} className="mt-1 flex w-full min-w-0 flex-col gap-1">
    {signoffs.signed.map(s => <p key={`${s.ref}-${s.at}`} className="m-0 break-words text-xs text-muted-foreground" data-signed-off={s.ref}>{signedLine(s)}</p>)}
    {open.map(ref => <SignOffButton key={ref} node={signoffs.node} label={label} signoffRef={ref} named={signoffs.refs.length > 1} />)}
  </div>
}
