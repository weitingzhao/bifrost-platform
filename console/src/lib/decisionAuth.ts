import type { AuthCapabilities } from '@/api/matrixTypes'
import { getPlatformOperatorToken } from '@/lib/platformAuth'

export const ADMIN_SIGN_IN_REFUSED =
  'The sign-in at the top right was not accepted as admin. It may have been replaced. Sign in again.'

export const DECISION_ROLE_UNKNOWN = 'Unknown — could not confirm who is signed in'

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

/** Server refused because the caller was not admin. The token itself is not in this text. */
export function isAdminRoleRefusal(message: string): boolean {
  return /admin token required/i.test(message)
}

/**
 * Raw server text for the smaller line under a role refusal.
 * If that text contains the session token, it is not shown.
 */
export function refusalDetail(message: string): string {
  const token = getPlatformOperatorToken()
  if (token !== '' && message.includes(token)) return 'admin token required'
  return message
}

export type DecisionGate =
  | { kind: 'admin'; token: string }
  | { kind: 'known'; role: string }
  | { kind: 'unknown' }

type Published = { kind: 'admin'; token: string } | { kind: 'blocked' }

let published: Published = { kind: 'blocked' }

/** Remember the role from the latest decision-page render. A click reads this, then the live token. */
export function publishDecisionGate(gate: DecisionGate): void {
  published = gate.kind === 'admin' ? { kind: 'admin', token: gate.token } : { kind: 'blocked' }
}

/**
 * Session token to send with a decision, or null when this call must not leave the browser.
 * The published role and the token in storage have to be the same admin session.
 */
export function adminSessionToken(): string | null {
  if (published.kind !== 'admin' || published.token === '') return null
  const live = getPlatformOperatorToken()
  if (live === '' || live !== published.token) return null
  return live
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
 * Who may decide. Admin only when the capability read says so and the stored token
 * is still the one that read was for. Loading, a failed read, or a mismatch is unknown.
 */
export function deriveDecisionGate(auth: {
  token: string
  caps: AuthCapabilities | undefined
  capsLoading: boolean
}): DecisionGate {
  if (auth.capsLoading || auth.caps == null) return { kind: 'unknown' }
  const live = getPlatformOperatorToken()
  if (live !== auth.token.trim()) return { kind: 'unknown' }
  const caps = auth.caps
  const authenticated = caps.authenticated === true
  const roleIsAdmin = caps.role === 'admin'
  const canAdmin = caps.can_admin === true
  if (authenticated && roleIsAdmin && canAdmin) {
    if (live === '') return { kind: 'unknown' }
    return { kind: 'admin', token: live }
  }
  if (authenticated && roleIsAdmin !== canAdmin) return { kind: 'unknown' }
  return { kind: 'known', role: signedInRoleLabel(auth.token, caps) }
}
