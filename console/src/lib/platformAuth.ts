export const PLATFORM_TOKEN_KEY = 'platform_operator_token'

/** User token (localStorage) wins over VITE_PLATFORM_OPERATOR_TOKEN env fallback. */
export function getPlatformOperatorToken(): string {
  if (typeof window === 'undefined') return ''
  const stored = window.localStorage.getItem(PLATFORM_TOKEN_KEY)?.trim() ?? ''
  if (stored !== '') return stored
  const envToken = (import.meta.env.VITE_PLATFORM_OPERATOR_TOKEN as string | undefined)?.trim()
  return envToken ?? ''
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
