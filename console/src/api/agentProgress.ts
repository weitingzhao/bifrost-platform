import { boundDocumentText, sanitizeDocumentText } from '@/pages/shell/progress/progressPart'

/*
 * What is left of the coordination progress document (GET /api/v1/agent/progress,
 * W-62): its publisher and route were removed on 2026-11-17 (dead since
 * 2026-10-20; where each line stands is Work › Lines). The poll interval and
 * the action link rule stay: the agents pages and questions use them.
 */

/** How often the agents pages re-read their runtime data. */
export const AGENT_PROGRESS_POLL_MS = 15_000

/** Hosts an action's link may open on (http(s), default port). */
const TRUSTED_PROGRESS_HOSTS = ['ops.bifrost.lan'] as const

/** An action with an optional link (a question's or a card's next step). */
export type ProgressAction = {
  text?: string
  done?: boolean
  link?: string
  link_text?: string
}

export type ActionLinkView =
  | { kind: 'anchor'; href: string; label: string; external: boolean }
  | { kind: 'blocked'; text: string; host: string }
  | { kind: 'text'; text: string }

/** The scheme's default port: none written, 443 on https, or 80 on http. */
function isDefaultPort(url: URL): boolean {
  if (url.port === '') return true
  if (url.protocol === 'https:' && url.port === '443') return true
  if (url.protocol === 'http:' && url.port === '80') return true
  return false
}

function shownHost(url: URL): string {
  const name = url.hostname.toLowerCase()
  if (url.port === '') return name
  return `${name}:${url.port}`
}

/** Same bound as an action's own text on the Needs you now card. */
const ACTION_LINK_LABEL_MAX = 200

/**
 * Visible label of an anchor. Prefer link text, then the action text, then the
 * link itself. Each candidate goes through the shared document cleaner; the
 * one that is shown is bounded like other action text. Nothing left to read,
 * including a fallback that is only the hash mark, is the console's own
 * "Open link". The href stays the original link, never this label.
 */
function actionAnchorLabel(linkText: string, text: string, raw: string, where: string): string {
  const authored = sanitizeDocumentText(linkText) || sanitizeDocumentText(text)
  const fallback = sanitizeDocumentText(raw)
  const body = authored || (fallback === '#' ? '' : fallback)
  if (body === '') return 'Open link'
  return boundDocumentText(`${body} (${where})`, ACTION_LINK_LABEL_MAX)
}

/**
 * A link opens only as a same-page hash, or as http(s) on TRUSTED_PROGRESS_HOSTS
 * using that scheme's default port. The label names the destination. A trusted
 * host on any other port stays text, and the note includes the host and port.
 */
export function actionLink(action: Pick<ProgressAction, 'text' | 'link' | 'link_text'>): ActionLinkView {
  const text = action.text ?? ''
  const linkText = action.link_text?.trim() ?? ''
  const raw = action.link?.trim() ?? ''
  if (raw === '') return { kind: 'text', text }

  if (raw.startsWith('#')) {
    return {
      kind: 'anchor',
      href: raw,
      label: actionAnchorLabel(linkText, text, raw, 'this console'),
      external: false,
    }
  }

  let url: URL | null = null
  try {
    url = new URL(raw)
  } catch {
    // not a URL; shown as text
  }
  const host = url?.hostname.toLowerCase() ?? ''
  const trusted = (TRUSTED_PROGRESS_HOSTS as readonly string[]).includes(host)
  const http = url != null && (url.protocol === 'http:' || url.protocol === 'https:')
  if (url != null && http && trusted && isDefaultPort(url)) {
    url.username = ''
    url.password = ''
    return {
      kind: 'anchor',
      href: url.href,
      label: actionAnchorLabel(linkText, text, raw, host),
      external: true,
    }
  }
  return { kind: 'blocked', text, host: url != null && http ? shownHost(url) : host }
}
