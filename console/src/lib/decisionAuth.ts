import type { AuthCapabilities } from '@/api/matrixTypes'
import { getPlatformOperatorToken } from '@/lib/platformAuth'
import { currentSignInEpoch } from '@/lib/signInEpoch'

export const ADMIN_SIGN_IN_REFUSED =
  'The sign-in at the top right was not accepted as admin. It may have been replaced. Sign in again.'

export const DECISION_ROLE_UNKNOWN = 'Unknown — could not confirm who is signed in'

export const COULD_NOT_CONFIRM_SIGN_IN = 'Could not confirm who is signed in.'

const ROLE_REFUSAL_MARK = 'console-role-refusal:'

export function decisionNeedsAdmin(role: string): string {
  return `Deciding needs the admin sign-in. You are signed in as ${role}.`
}

/** A decision or answer was refused before any request left the browser. */
export class DecisionNotSent extends Error {
  constructor() {
    super('Decision was not sent')
    this.name = 'DecisionNotSent'
  }
}

export function isDecisionNotSent(err: unknown): boolean {
  return err instanceof DecisionNotSent
}

/** Server refused because the caller was not admin. Used only before the text is stored. */
export function isAdminRoleRefusal(message: string): boolean {
  return /admin token required/i.test(message)
}

/** What a role or authentication refusal stores: the status, never the server text. */
export function roleRefusalMark(status: number): string {
  return `${ROLE_REFUSAL_MARK}${status}`
}

export function roleRefusalStatus(message: string): number | null {
  if (!message.startsWith(ROLE_REFUSAL_MARK)) return null
  const status = Number(message.slice(ROLE_REFUSAL_MARK.length))
  return Number.isInteger(status) ? status : null
}

/**
 * Text that may be stored. Authentication and role refusals keep only the status.
 * Any other error is stripped of the token this request was sent with.
 * If that token cannot be removed, only the status is kept.
 */
export function decisionErrorText(status: number, serverText: string, sentToken: string): string {
  if (status === 401 || status === 403 || isAdminRoleRefusal(serverText)) {
    return roleRefusalMark(status)
  }
  if (sentToken === '') return `HTTP ${status}`
  if (!serverText.includes(sentToken)) return serverText
  const cleaned = serverText.split(sentToken).join('')
  if (cleaned.includes(sentToken)) return `HTTP ${status}`
  return cleaned
}

/** Drop a copied token from one field. Null means it could not be guaranteed gone. */
export function textWithoutSentToken(value: string, sentToken: string): string | null {
  if (sentToken === '') return null
  if (!value.includes(sentToken)) return value
  const cleaned = value.split(sentToken).join('')
  if (cleaned.includes(sentToken)) return null
  return cleaned
}

export type DecisionGate =
  | { kind: 'admin'; token: string; epoch: number }
  | { kind: 'known'; role: string }
  | { kind: 'unknown'; failed: boolean }

type Published = { kind: 'admin'; epoch: number; token: string } | { kind: 'blocked' }

let published: Published = { kind: 'blocked' }

/** Remember the role from the latest decision-page render. A click reads this, then the epoch. */
export function publishDecisionGate(gate: DecisionGate): void {
  const epoch = currentSignInEpoch()
  published =
    gate.kind === 'admin' && gate.token !== '' && gate.epoch === epoch.id && gate.token === epoch.token
      ? { kind: 'admin', epoch: gate.epoch, token: gate.token }
      : { kind: 'blocked' }
}

/**
 * Session token to send with a decision, or null when this call must not leave the browser.
 * The published role, the current epoch, and the token in storage have to be the same sign-in.
 */
export function adminSessionToken(): string | null {
  if (published.kind !== 'admin' || published.token === '') return null
  const epoch = currentSignInEpoch()
  if (epoch.id !== published.epoch || epoch.token !== published.token || epoch.token === '') return null
  const live = getPlatformOperatorToken()
  if (live === '' || live !== epoch.token) return null
  return epoch.token
}

/** Badge word for a confirmed non-admin session, in the same form as the top-right badge. */
export function signedInRoleLabel(token: string, caps: AuthCapabilities): string {
  if (token.trim() !== '' && caps.authenticated !== true) return 'INVALID'
  if (caps.authenticated === true) {
    const role = typeof caps.role === 'string' ? caps.role.trim() : ''
    if (role === '') return 'UNKNOWN'
    return role.toUpperCase()
  }
  return 'GUEST'
}

/**
 * Who may decide. Admin only for this epoch's own answer, and only when the
 * token about to be sent is the one that epoch captured. Anything else is
 * not admin, or unknown — never admin.
 */
export function deriveDecisionGate(auth: {
  token: string
  epoch: number
  caps: AuthCapabilities | undefined
  capsEpoch: number | null
  capsLoading: boolean
  failed: boolean
}): DecisionGate {
  if (auth.failed) return { kind: 'unknown', failed: true }
  if (auth.capsLoading || auth.caps == null || auth.capsEpoch == null) return { kind: 'unknown', failed: false }
  if (auth.capsEpoch !== auth.epoch) return { kind: 'unknown', failed: false }
  const epoch = currentSignInEpoch()
  if (epoch.id !== auth.epoch || epoch.token !== auth.token) return { kind: 'unknown', failed: false }
  const live = getPlatformOperatorToken()
  if (live !== epoch.token) return { kind: 'unknown', failed: false }
  const caps = auth.caps
  const authenticated = caps.authenticated === true
  const roleIsAdmin = caps.role === 'admin'
  const canAdmin = caps.can_admin === true
  if (authenticated && roleIsAdmin && canAdmin) {
    if (live === '') return { kind: 'unknown', failed: false }
    return { kind: 'admin', token: epoch.token, epoch: epoch.id }
  }
  if (authenticated && roleIsAdmin !== canAdmin) return { kind: 'unknown', failed: false }
  return { kind: 'known', role: signedInRoleLabel(auth.token, caps) }
}
