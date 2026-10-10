/** The Console deep link of one approval; same form as approvalnotify.ConsoleClickPrefix. */
export function consoleApprovalUrl(id: string, env: NodeJS.ProcessEnv = process.env): string {
  const base = (env.PLATFORM_CONSOLE_URL?.trim() || 'http://ops.bifrost.lan').replace(/\/$/, '')
  return `${base}/#approvals?id=${encodeURIComponent(id)}`
}

/**
 * Where the Owner decides a new request. Sessions without bifrost-approve
 * (Cursor, by design) only pass this on; they cannot approve. Tier D is
 * decided on Console only (ADR §5, Owner 2026-10-10).
 */
export function approveHint(id: string, number?: number, tier?: string, env: NodeJS.ProcessEnv = process.env): string {
  const ref = number ? `#${number}` : id
  const url = consoleApprovalUrl(id, env)
  if (tier === 'D') {
    return `Tier D: the Owner approves ${ref} on Console only, at ${url}. Chat (批 ${ref}) does not accept tier D.`
  }
  return `Owner approves ${ref} on the phone or at ${url}. In a Claude chat, 批 ${ref} opens the bifrost-approve permission prompt; chat text alone is not an approval.`
}

/** Adds approve_hint to a created approval body (one with an id). */
export function withApproveHint(body: unknown, env: NodeJS.ProcessEnv = process.env): unknown {
  if (body == null || typeof body !== 'object' || Array.isArray(body)) return body
  const rec = body as { id?: unknown; number?: unknown; tier?: unknown }
  if (typeof rec.id !== 'string' || rec.id === '') return body
  const number = typeof rec.number === 'number' ? rec.number : undefined
  const tier = typeof rec.tier === 'string' ? rec.tier : undefined
  return { ...rec, approve_hint: approveHint(rec.id, number, tier, env) }
}
