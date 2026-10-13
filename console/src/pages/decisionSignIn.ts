import type { PlatformAuthContextValue } from '@/hooks/platformAuthContext'

/** Shown while the same capabilities read the top-right badge uses is still in flight. */
export const SIGN_IN_CHECKING = 'Checking your sign-in…'

/** The capabilities read failed. This is not "signed out" and not "admin". */
export const SIGN_IN_UNREADABLE = 'Could not read your sign-in. Decisions stay unavailable until it can be read.'

export const SIGN_IN_ABSENT = 'Not signed in. Sign in at the top right with the admin token to decide.'

export const SIGN_IN_UNRECOGNIZED =
  'This sign-in was not recognized. Sign in at the top right with the admin token to decide.'

export const SIGN_IN_OPERATOR =
  'Signed in as operator, not admin. Sign in at the top right with the admin token to decide.'

export const SIGN_IN_NOT_ADMIN =
  'This sign-in is not an admin. Sign in at the top right with the admin token to decide.'

export const DECISION_REJECTED =
  'The server did not accept this sign-in as admin. Sign in again at the top right with the current admin token.'

type SignInAuth = Pick<PlatformAuthContextValue, 'token' | 'caps' | 'capsLoading' | 'capsError' | 'canAdmin'>

/**
 * Why a decision control must stay off, or null when the server says this sign-in is admin.
 * The role comes from the capabilities query behind the top-right badge. The token string is
 * only "is there a sign-in", never a guess about which role it is.
 */
export function decisionBlockReason(auth: SignInAuth): string | null {
  if (auth.capsLoading) return SIGN_IN_CHECKING
  if (auth.capsError) return SIGN_IN_UNREADABLE
  if (auth.token.trim() === '') return SIGN_IN_ABSENT
  if (auth.caps?.authenticated !== true) return SIGN_IN_UNRECOGNIZED
  if (auth.canAdmin) return null
  if ((auth.caps.role ?? '').toLowerCase() === 'operator') return SIGN_IN_OPERATOR
  return SIGN_IN_NOT_ADMIN
}

/** Keep a saved token out of any sentence the page draws. */
export function scrubSecret(text: string, secret: string): string {
  const value = secret.trim()
  if (value === '') return text
  return text.split(value).join('')
}
