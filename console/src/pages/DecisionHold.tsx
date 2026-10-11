import { Button } from '@bifrost/ui'
import {
  ADMIN_SIGN_IN_REFUSED,
  COULD_NOT_CONFIRM_SIGN_IN,
  DECISION_ROLE_UNKNOWN,
  decisionNeedsAdmin,
  roleRefusalStatus,
  type DecisionGate,
} from '@/lib/decisionAuth'
import { requestSignIn } from '@/lib/signInRequest'

/**
 * Who can decide, shown beside the controls before they are pressed.
 * A server refusal for authentication or role names the top-right sign-in
 * and the HTTP status. The server's text is not shown.
 */
export function DecisionHold({
  gate,
  refusal,
  onRetry,
}: {
  gate: DecisionGate
  refusal: string | null
  onRetry?: () => void
}) {
  const status = refusal != null && refusal !== '' ? roleRefusalStatus(refusal) : null
  const refused = status != null
  const failed = gate.kind === 'unknown' && gate.failed
  if (gate.kind === 'admin' && !refused) return null
  const line = refused
    ? ADMIN_SIGN_IN_REFUSED
    : gate.kind === 'known'
      ? decisionNeedsAdmin(gate.role)
      : DECISION_ROLE_UNKNOWN
  const offerSignIn = refused || gate.kind === 'known' || failed
  return (
    <div className="flex w-full min-w-0 flex-col gap-2" data-decision-failure={refusal ?? ''}>
      <p role="status" className="m-0 text-sm">
        {line}
      </p>
      {failed ? <p className="m-0 text-sm">{COULD_NOT_CONFIRM_SIGN_IN}</p> : null}
      {refused ? (
        <p data-refusal-status className="m-0 text-sm">
          {`HTTP ${status}`}
        </p>
      ) : null}
      {failed ? (
        <Button type="button" variant="outline" className="w-full sm:w-auto" onClick={() => onRetry?.()}>
          Try again
        </Button>
      ) : null}
      {offerSignIn ? (
        <Button type="button" variant="outline" className="w-full sm:w-auto" onClick={() => requestSignIn()}>
          Sign in
        </Button>
      ) : null}
    </div>
  )
}
