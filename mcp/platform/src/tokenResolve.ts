/**
 * PLATFORM_TOKEN_ENV_KEY pins a bridge to one role (the read-only bridges name the
 * viewer key): only that key is read, so an inherited operator or admin token
 * cannot stand in for it (TD-225). `fallback` reads a key from `.env`, and is a
 * no-op for a non-loopback PLATFORM_API_URL.
 */
export function resolveTokenFrom(
  env: Record<string, string | undefined>,
  fallback: (key: string) => string,
): string {
  const pinnedKey = env.PLATFORM_TOKEN_ENV_KEY?.trim() || ''
  if (pinnedKey) return env[pinnedKey]?.trim() || fallback(pinnedKey)
  return env.PLATFORM_OPERATOR_TOKEN?.trim() || env.PLATFORM_ADMIN_TOKEN?.trim() || fallback('PLATFORM_OPERATOR_TOKEN')
}
