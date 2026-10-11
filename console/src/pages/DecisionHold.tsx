import { Button } from '@bifrost/ui'
import {
  ADMIN_SIGN_IN_REFUSED,
  DECISION_ROLE_UNKNOWN,
  decisionNeedsAdmin,
  isAdminRoleRefusal,
  refusalDetail,
  type DecisionGate,
} from '@/lib/decisionAuth'
import { requestSignIn } from '@/lib/signInRequest'

/**
 * Who can decide, shown beside the controls before they are pressed.
 * A server refusal for lack of admin names the top-right sign-in.
 */
export function DecisionHold({ gate, refusal }: { gate: DecisionGate; refusal: string | null }) {
  const refused = refusal != null && refusal !== '' && isAdminRoleRefusal(refusal)
  if (gate.kind === 'admin' && !refused) return null
  const line = refused
    ? ADMIN_SIGN_IN_REFUSED
    : gate.kind === 'known'
      ? decisionNeedsAdmin(gate.role)
      : DECISION_ROLE_UNKNOWN
  const offerSignIn = refused || gate.kind === 'known'
  return (
    <div className="flex w-full min-w-0 flex-col gap-2">
      <p role="status" className="m-0 text-sm">
        {line}
      </p>
      {refused ? (
        <p data-refusal-detail className="m-0 text-xs text-muted-foreground">
          {refusalDetail(refusal)}
        </p>
      ) : null}
      {offerSignIn ? (
        <Button type="button" variant="outline" className="w-full sm:w-auto" onClick={() => requestSignIn()}>
          Sign in
        </Button>
      ) : null}
    </div>
  )
}
