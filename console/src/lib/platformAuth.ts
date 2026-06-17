export const PLATFORM_TOKEN_KEY = 'platform_operator_token'

/**
 * Resolve bearer token for Console actuation calls.
 * - localStorage key present (including empty string) → use stored value only (sign-out sets "").
 * - localStorage key absent → fall back to VITE_PLATFORM_OPERATOR_TOKEN for dev convenience.
 */
export function getPlatformOperatorToken(): string {
  if (typeof window === 'undefined') return ''
  const raw = window.localStorage.getItem(PLATFORM_TOKEN_KEY)
  if (raw !== null) {
    return raw.trim()
  }
  const envToken = (import.meta.env.VITE_PLATFORM_OPERATOR_TOKEN as string | undefined)?.trim()
  return envToken ?? ''
}

export function setPlatformOperatorToken(token: string): void {
  if (typeof window === 'undefined') return
  const trimmed = token.trim()
  if (trimmed !== '') {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, trimmed)
  } else {
    // Empty string suppresses env fallback until user connects again.
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, '')
  }
}

/** Clear session override so dev env token can apply again (optional reset). */
export function clearPlatformOperatorTokenOverride(): void {
  if (typeof window === 'undefined') return
  window.localStorage.removeItem(PLATFORM_TOKEN_KEY)
}
