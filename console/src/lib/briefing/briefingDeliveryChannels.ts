import type { WorkIntent } from '@/lib/briefing/workIntents'

/** Intents where Ops runner (Agent Desk) tools align with the session pack. */
const AGENT_DESK_SUITED_INTENTS: ReadonlySet<WorkIntent> = new Set([
  'ops',
  'debug',
  'cluster',
  'automate',
  'release',
])

export function isAgentDeskSuitedIntent(intent: WorkIntent): boolean {
  return AGENT_DESK_SUITED_INTENTS.has(intent)
}

export function agentDeskPrefillDisabledReason(intent: WorkIntent): string | undefined {
  if (isAgentDeskSuitedIntent(intent)) return undefined
  return 'Feature, frontend, and business briefings need the full multi-repo Cursor IDE workspace — use Open in Cursor or Copy session pack.'
}

export const BRIEFING_IDE_DELIVERY_HINT =
  'Primary: Open in Cursor (deep link + /briefing command) from an external browser. ' +
  'Pack is written to data/briefing/active-pack.md. ' +
  'If you are already inside Cursor Agent Browser, Prepare pack only — run /briefing in the current chat (do not deep-link again). ' +
  'Fallback: Copy session pack into a new Cursor chat.'

export const BRIEFING_AGENT_DESK_DELIVERY_HINT =
  'Optional: prefill Agent Desk for the Mac Mini Ops runner (Cursor SDK). Suited to short cluster/debug/release tasks — not a substitute for IDE for multi-repo development.'

/** Cooldown after a Cursor deep-link open — prevents double-click / Browser re-fire loops. */
export const CURSOR_BRIEFING_DEEPLINK_COOLDOWN_MS = 12_000

const DEEPLINK_COOLDOWN_STORAGE_KEY = 'bifrost_briefing_cursor_deeplink_at'

/** Cursor prompt deep link that prefills the /briefing slash command. */
export function cursorBriefingDeeplink(): string {
  return (
    'cursor://anysphere.cursor-deeplink/prompt?text=' + encodeURIComponent('/briefing')
  )
}

/**
 * True when the Console is running inside Cursor / Electron (Agent Browser, Simple Browser).
 * Opening cursor:// from here spawns another Agent with /briefing and can loop.
 */
export function isLikelyCursorIdeBrowser(): boolean {
  if (typeof window === 'undefined' || typeof navigator === 'undefined') return false
  const ua = navigator.userAgent
  if (/Cursor/i.test(ua)) return true
  if (/Electron/i.test(ua)) return true
  try {
    // vscode / Cursor webview often embeds the page cross-origin
    if (window.self !== window.top) return true
  } catch {
    return true
  }
  return false
}

export function cursorDeeplinkCooldownRemainingMs(now = Date.now()): number {
  if (typeof sessionStorage === 'undefined') return 0
  try {
    const raw = sessionStorage.getItem(DEEPLINK_COOLDOWN_STORAGE_KEY)
    if (raw == null || raw === '') return 0
    const at = Number(raw)
    if (!Number.isFinite(at)) return 0
    return Math.max(0, CURSOR_BRIEFING_DEEPLINK_COOLDOWN_MS - (now - at))
  } catch {
    return 0
  }
}

function markCursorDeeplinkOpened(now = Date.now()): void {
  if (typeof sessionStorage === 'undefined') return
  try {
    sessionStorage.setItem(DEEPLINK_COOLDOWN_STORAGE_KEY, String(now))
  } catch {
    // ignore quota / private mode
  }
}

export type CursorBriefingLaunchMode = 'deeplink' | 'prepare_only'

export type CursorBriefingLaunchResult = {
  mode: CursorBriefingLaunchMode
  /** Human-readable status for the Session CTA strip. */
  status: string
}

/**
 * After pack prepare: open Cursor deep link only when safe (external browser + outside cooldown).
 * Inside Cursor IDE Browser → prepare-only to avoid /briefing Agent spawn loops.
 */
export function launchCursorBriefingAfterPrepare(): CursorBriefingLaunchResult {
  if (isLikelyCursorIdeBrowser()) {
    return {
      mode: 'prepare_only',
      status:
        'Pack ready — already in Cursor. Run /briefing in this chat (do not Open in Cursor again).',
    }
  }

  const remaining = cursorDeeplinkCooldownRemainingMs()
  if (remaining > 0) {
    const secs = Math.ceil(remaining / 1000)
    return {
      mode: 'prepare_only',
      status: `Pack ready — deep link cooling down (${secs}s). Run /briefing in an existing Agent chat.`,
    }
  }

  markCursorDeeplinkOpened()
  window.open(cursorBriefingDeeplink())
  return {
    mode: 'deeplink',
    status: 'Pack prepared — opening Cursor IDE (/briefing). Press Enter to start.',
  }
}

/** Button label: avoid implying a new Agent when already inside Cursor. */
export function openInCursorButtonLabel(opts: {
  preparing: boolean
  dataReady: boolean
  packBlocked: boolean
  insideCursor: boolean
  /** Session already prepared/copied pack this turn. */
  packReady?: boolean
}): string {
  if (opts.preparing) return 'Preparing…'
  if (!opts.dataReady) return 'Loading…'
  if (opts.packBlocked) return 'Pack blocked'
  if (opts.insideCursor) {
    return opts.packReady ? 'Re-prepare pack' : 'Prepare pack'
  }
  return opts.packReady ? 'Re-open in Cursor' : 'Open in Cursor'
}
