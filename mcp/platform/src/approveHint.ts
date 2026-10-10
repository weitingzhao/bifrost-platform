/**
 * Where the Owner decides a new request. Sessions without bifrost-approve
 * (Cursor, by design) only pass this on; they cannot approve.
 */
export function approveHint(id: string, number?: number, env: NodeJS.ProcessEnv = process.env): string {
  const base = (env.PLATFORM_CONSOLE_URL?.trim() || 'http://ops.bifrost.lan').replace(/\/$/, '')
  const ref = number ? `#${number}` : id
  return `Owner approves ${ref} on the phone or at ${base}/#approvals?id=${encodeURIComponent(id)}. In a Claude chat, 批 ${ref} opens the bifrost-approve permission prompt; chat text alone is not an approval.`
}

/** Adds approve_hint to a created approval body (one with an id). */
export function withApproveHint(body: unknown, env: NodeJS.ProcessEnv = process.env): unknown {
  if (body == null || typeof body !== 'object' || Array.isArray(body)) return body
  const rec = body as { id?: unknown; number?: unknown }
  if (typeof rec.id !== 'string' || rec.id === '') return body
  return { ...rec, approve_hint: approveHint(rec.id, typeof rec.number === 'number' ? rec.number : undefined, env) }
}
