export const PLATFORM_TOKEN_KEY = 'platform_operator_token'

/**
 * Resolve bearer token for Console actuation calls — only what the user entered via
 * Connect (localStorage). There is no build-time fallback: a VITE_* token is baked into
 * the JS that the dev server hands to every browser on the LAN.
 */
export function getPlatformOperatorToken(): string {
  if (typeof window === 'undefined') return ''
  return window.localStorage.getItem(PLATFORM_TOKEN_KEY)?.trim() ?? ''
}

export function setPlatformOperatorToken(token: string): void {
  if (typeof window === 'undefined') return
  const trimmed = token.trim()
  if (trimmed !== '') {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, trimmed)
  } else {
    window.localStorage.removeItem(PLATFORM_TOKEN_KEY)
  }
}

export function clearPlatformOperatorTokenOverride(): void {
  if (typeof window === 'undefined') return
  window.localStorage.removeItem(PLATFORM_TOKEN_KEY)
}
