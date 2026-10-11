import { createHash } from 'node:crypto'

/** Same canonical form as api/internal/questions. Vectors live next to the Go tests. */

const BIDI = new Set([0x061c, 0x200e, 0x200f, 0x202a, 0x202b, 0x202c, 0x202d, 0x202e, 0x2066, 0x2067, 0x2068, 0x2069])
const ZERO = new Set([0x180e, 0x200b, 0x200c, 0x200d, 0x2060, 0xfeff])

// Same classes as api/internal/notice/redact.go. Whitespace is space, tab, LF, CR,
// and FF. A word edge is the start or end of the text, or one character outside
// [A-Za-z0-9_]. Letters are listed in both cases. Where a pattern matches a letter
// without regard to case, U+017F counts as s and U+212A counts as k: in keywords,
// in the bearer and basic schemes, and in the character classes of the secret.
// Characters outside the redacted span are copied unchanged. Do not use \s, \w,
// \b, look-around, or the i flag. Bounded rules match the secret only; the edges
// are checked outside the pattern and the scan resumes at the end of the match.
const REDACT: Array<{ re: RegExp; repl: string; bound: boolean }> = [
  { re: /-----BEGIN [A-Z ]*PRIVATE KEY-----[\0-\uFFFF]*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)/g, repl: '[redacted private key]', bound: false },
  { re: /([Bb][Ee][Aa][Rr][Ee][Rr]|[Bb][Aa][Ss\u017F][Ii][Cc])[ \t\n\r\f]+[A-Za-z\u017F\u212A0-9._~+/=-]{8,}/g, repl: '$1 [redacted]', bound: true },
  { re: /([A-Za-z\u017F\u212A0-9_.-]*(?:[Pp][Aa][Ss\u017F][Ss\u017F][Ww][Oo][Rr][Dd]|[Pp][Aa][Ss\u017F][Ss\u017F][Ww][Dd]|[Pp][Ww][Dd]|[Ss\u017F][Ee][Cc][Rr][Ee][Tt]|[Tt][Oo][Kk\u212A][Ee][Nn]|[Aa][Pp][Ii][_-]?[Kk\u212A][Ee][Yy]|[Aa][Cc][Cc][Ee][Ss\u017F][Ss\u017F][_-]?[Kk\u212A][Ee][Yy]|[Pp][Rr][Ii][Vv][Aa][Tt][Ee][_-]?[Kk\u212A][Ee][Yy]|[Cc][Rr][Ee][Dd][Ee][Nn][Tt][Ii][Aa][Ll])[A-Za-z\u017F\u212A0-9_.-]*)(["']?[ \t\n\r\f]*[:=][ \t\n\r\f]*["']?)[^ \t\n\r\f"',;]+/g, repl: '$1$2[redacted]', bound: true },
  { re: /eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}/g, repl: '[redacted jwt]', bound: true },
  { re: /(?:AKIA|ASIA)[0-9A-Z]{16}/g, repl: '[redacted aws key]', bound: true },
  { re: /(?:gh[pousr]_[A-Za-z0-9]{20,}[A-Za-z0-9_]*|glpat-[A-Za-z0-9_-]{20,}|xox[abpr]-[A-Za-z0-9-]{10,}[A-Za-z0-9_]*)/g, repl: '[redacted token]', bound: true },
  { re: /:\/\/([^/ \t\n\r\f:@]+):([^@ \t\n\r\f/]+)@/g, repl: '://$1:[redacted]@', bound: false },
]

function asciiWord(code: number): boolean {
  return code === 95 || (code >= 48 && code <= 57) || (code >= 65 && code <= 90) || (code >= 97 && code <= 122)
}

function leftEdge(s: string, i: number): boolean {
  if (i <= 0) return true
  return !asciiWord(s.charCodeAt(i - 1))
}

function rightEdge(s: string, i: number): boolean {
  if (i >= s.length) return true
  return !asciiWord(s.charCodeAt(i))
}

function expand(m: RegExpExecArray, repl: string): string {
  return repl.replace(/\$(\d)/g, (_, n) => m[Number(n)] ?? '')
}

// replaceBounded is the same scan as notice.replaceBounded: a candidate is
// kept only when both sides are edges, and the next search starts at the end
// of the match rather than after a consumed separator.
function replaceBounded(s: string, re: RegExp, repl: string): string {
  const flags = re.flags.includes('g') ? re.flags : `${re.flags}g`
  const r = new RegExp(re.source, flags.replace('g', ''))
  let out = ''
  let i = 0
  while (i < s.length) {
    r.lastIndex = 0
    const m = r.exec(s.slice(i))
    if (!m) {
      out += s.slice(i)
      break
    }
    const start = i + m.index
    const end = start + m[0].length
    if (end > start && leftEdge(s, start) && rightEdge(s, end)) {
      out += s.slice(i, start) + expand(m, repl)
      i = end
      continue
    }
    let next = start + 1
    if (next <= i) next = i + 1
    if (next > s.length) {
      out += s.slice(i)
      break
    }
    out += s.slice(i, next)
    i = next
  }
  return out
}

/** Masks the same secret shapes as api/internal/notice.Redact. */
export function redact(s: string): string {
  let out = s
  for (const rule of REDACT) {
    out = rule.bound ? replaceBounded(out, rule.re, rule.repl) : out.replace(rule.re, rule.repl)
  }
  return out
}

export class CanonicalError extends Error {
  constructor(readonly field: string, message: string) {
    super(message)
  }
}

export interface Canonical {
  json: string
  requestFP: string
  key: string
}

export function canonicalizeObject(input: unknown): Canonical {
  if (input == null || typeof input !== 'object' || Array.isArray(input)) {
    throw new CanonicalError('body', 'invalid json body')
  }
  const raw = input as Record<string, unknown>
  const known = new Set([
    'idempotency_key', 'title', 'body', 'options', 'recommended', 'allow_text', 'urgency', 'meanwhile',
    'work_id', 'requester_thread', 'link', 'link_text', 'ttl_hours',
  ])
  for (const key of Object.keys(raw)) {
    if (!known.has(key)) throw new CanonicalError(key, `unknown field ${key}`)
  }
  const title = requiredLine(raw.title, 'title', 300)
  const body = optionalText(raw.body, 'body', 3000, true)
  const options = buildOptions(raw.options)
  const recommended = recommendedIndex(raw.recommended, options.length)
  let allow = options.length === 0
  if (Object.prototype.hasOwnProperty.call(raw, 'allow_text') && raw.allow_text != null) {
    if (typeof raw.allow_text !== 'boolean') throw new CanonicalError('allow_text', 'allow_text must be a boolean')
    if (options.length === 0 && raw.allow_text === false) {
      throw new CanonicalError('allow_text', 'allow_text must be true when there are no options')
    }
    if (options.length > 0) allow = raw.allow_text
  }
  const urgency = requiredLine(raw.urgency, 'urgency', 300)
  const meanwhile = requiredLine(raw.meanwhile, 'meanwhile', 300)
  const work = optionalLine(raw.work_id, 'work_id', 40)
  if (work && !/^(W-[0-9]+|TD-[0-9]+|LANE-[A-Za-z0-9-]+)$/.test(work)) {
    throw new CanonicalError('work_id', 'work_id is invalid')
  }
  const thread = optionalLine(raw.requester_thread, 'requester_thread', 120)
  const link = optionalLine(raw.link, 'link', 300)
  if (linkForbidden(link)) throw new CanonicalError('link', 'link is not allowed')
  let linkText = optionalLine(raw.link_text, 'link_text', 160)
  if (!link) linkText = ''
  let ttl = 24
  if (Object.prototype.hasOwnProperty.call(raw, 'ttl_hours') && raw.ttl_hours != null) {
    ttl = integer(raw.ttl_hours, 'ttl_hours')
    if (ttl < 1 || ttl > 168) throw new CanonicalError('ttl_hours', 'ttl_hours must be from 1 to 168')
  }
  let key = ''
  if (Object.prototype.hasOwnProperty.call(raw, 'idempotency_key') && raw.idempotency_key != null) {
    if (typeof raw.idempotency_key !== 'string') throw new CanonicalError('idempotency_key', 'idempotency_key must be a string')
    key = raw.idempotency_key
  }
  const doc: Record<string, unknown> = { allow_text: allow, meanwhile, title, ttl_hours: ttl, urgency }
  if (body) doc.body = body
  if (options.length) doc.options = options.map((o) => (o.detail ? { label: o.label, detail: o.detail } : { label: o.label }))
  if (recommended > 0) doc.recommended = recommended
  if (thread) doc.requester_thread = thread
  if (work) doc.work_id = work
  if (link) doc.link = link
  if (linkText) doc.link_text = linkText
  const json = canonicalJSON(doc)
  return { json, requestFP: sha256hex(json), key }
}

/** hex(sha256(canonical({session, request}))). session is not trimmed. */
export function defaultKey(session: string, requestCanonical: string): string {
  return sha256hex(`{"request":${requestCanonical},"session":${canonicalJSON(session)}}`)
}

function requiredLine(value: unknown, field: string, limit: number): string {
  const s = line(value, field, limit, false)
  if (!s) throw new CanonicalError(field, `${field} is required`)
  return s
}

function optionalLine(value: unknown, field: string, limit: number): string {
  return line(value, field, limit, false)
}

function optionalText(value: unknown, field: string, limit: number, keepNewlines: boolean): string {
  return line(value, field, limit, keepNewlines)
}

function line(value: unknown, field: string, limit: number, keepNewlines: boolean): string {
  if (value == null) return ''
  if (typeof value !== 'string') throw new CanonicalError(field, `${field} must be a string`)
  const s = clean(value, keepNewlines)
  if (s && encodedBytes(s) > limit) throw new CanonicalError(field, `${field} exceeds ${limit} bytes`)
  return s
}

function integer(value: unknown, field: string): number {
  if (typeof value !== 'number' || !Number.isInteger(value)) throw new CanonicalError(field, `${field} must be an integer`)
  return value
}

function recommendedIndex(value: unknown, count: number): number {
  if (value == null) return 0
  const n = integer(value, 'recommended')
  if (n !== 0 && (n < 1 || n > count)) throw new CanonicalError('recommended', 'recommended is out of range')
  if (n < 0) throw new CanonicalError('recommended', 'recommended is out of range')
  return n
}

function buildOptions(value: unknown): Array<{ label: string; detail: string }> {
  if (value == null) return []
  if (!Array.isArray(value)) throw new CanonicalError('options', 'options must be a list')
  if (value.length === 0) return []
  if (value.length < 2 || value.length > 4) throw new CanonicalError('options', 'options must be none or 2 to 4')
  return value.map((row) => {
    if (row == null || typeof row !== 'object' || Array.isArray(row)) throw new CanonicalError('options', 'options must be a list')
    const rec = row as Record<string, unknown>
    for (const key of Object.keys(rec)) {
      if (key !== 'label' && key !== 'detail') throw new CanonicalError(key, `unknown field ${key}`)
    }
    const label = requiredLine(rec.label, 'label', 160)
    const detail = optionalLine(rec.detail, 'detail', 300)
    return { label, detail }
  })
}

export function clean(s: string, keepNewlines: boolean): string {
  let out = ''
  for (const ch of s) {
    const r = ch.codePointAt(0) ?? 0
    if (ch === '\n' || ch === '\r') {
      if (keepNewlines) out += ch
      continue
    }
    if (r === 0x2028 || r === 0x2029 || BIDI.has(r) || ZERO.has(r)) continue
    if (r < 0x20 || (r >= 0x7f && r <= 0x9f)) continue
    out += ch
  }
  out = redact(out)
  if (keepNewlines) out = out.replace(/\r\n/g, '\n').replace(/\r/g, '\n')
  return out.trim()
}

function linkForbidden(raw: string): boolean {
  const s = raw.trim()
  if (!s) return false
  if (segmentForbidden(fragmentSegment(s))) return true
  try {
    const u = new URL(s)
    const parts = u.pathname.split('/').filter(Boolean)
    const last = parts.length ? parts[parts.length - 1].toLowerCase() : ''
    return segmentForbidden(last)
  } catch {
    return false
  }
}

function fragmentSegment(s: string): string {
  const i = s.indexOf('#')
  if (i < 0) return ''
  let frag = s.slice(i + 1)
  const q = frag.indexOf('?')
  if (q >= 0) frag = frag.slice(0, q)
  try {
    frag = decodeURIComponent(frag)
  } catch {
    /* keep the raw fragment */
  }
  return frag.replace(/^\/+|\/+$/g, '').toLowerCase()
}

function segmentForbidden(seg: string): boolean {
  return seg === 'approvals' || seg === 'maintenance' || seg === 'questions'
}

function encodedBytes(s: string): number {
  return Buffer.byteLength(canonicalJSON(s), 'utf8')
}

function canonicalJSON(value: unknown): string {
  if (value === null) return 'null'
  if (typeof value === 'string' || typeof value === 'boolean' || typeof value === 'number') return JSON.stringify(value)
  if (Array.isArray(value)) return `[${value.map(canonicalJSON).join(',')}]`
  if (typeof value === 'object') {
    const obj = value as Record<string, unknown>
    const keys = Object.keys(obj).sort()
    return `{${keys.map((k) => `${JSON.stringify(k)}:${canonicalJSON(obj[k])}`).join(',')}}`
  }
  throw new CanonicalError('body', 'invalid json body')
}

function sha256hex(s: string): string {
  return createHash('sha256').update(s).digest('hex')
}
