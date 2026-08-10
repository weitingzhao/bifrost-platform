export type SessionLogTone = 'ok' | 'warn' | 'error' | 'dim'

/** Max lines requested from API (UI never accumulates unbounded). */
export const DOCK_SESSION_LOG_LINES_TILED = 80
export const DOCK_SESSION_LOG_LINES_MAXIMIZED = 200

/**
 * Lightweight whole-line tone for bdev / chi / vite log tails.
 * Avoids matching years like 2026 — HTTP status uses " - 200 " shape.
 */
export function sessionLogLineTone(line: string): SessionLogTone | null {
  if (line.trim() === '') return 'dim'
  if (/\bERROR\b/i.test(line) || /\bFATAL\b/i.test(line) || /\bpanic\b/i.test(line)) {
    return 'error'
  }
  if (/\bWARN(?:ING)?\b/i.test(line)) return 'warn'

  const http = line.match(/\s-\s([1-5]\d{2})\s/)
  if (http != null) {
    const code = Number(http[1])
    if (code >= 500) return 'error'
    if (code >= 400) return 'warn'
    if (code >= 200 && code < 300) return 'ok'
  }
  return null
}
