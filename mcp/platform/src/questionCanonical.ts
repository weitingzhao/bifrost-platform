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
// are checked outside the pattern. A failed edge resumes one character later.
// Bearer, jwt, AWS and github/slack walk those starts in a few passes, so a
// long "Zxoxb-" tail is not re-scanned. Keyword assignments walk once.
const KEYWORD_HIT = /(?:[Pp][Aa][Ss\u017F][Ss\u017F][Ww][Oo][Rr][Dd]|[Pp][Aa][Ss\u017F][Ss\u017F][Ww][Dd]|[Pp][Ww][Dd]|[Ss\u017F][Ee][Cc][Rr][Ee][Tt]|[Tt][Oo][Kk\u212A][Ee][Nn]|[Aa][Pp][Ii][_-]?[Kk\u212A][Ee][Yy]|[Aa][Cc][Cc][Ee][Ss\u017F][Ss\u017F][_-]?[Kk\u212A][Ee][Yy]|[Pp][Rr][Ii][Vv][Aa][Tt][Ee][_-]?[Kk\u212A][Ee][Yy]|[Cc][Rr][Ee][Dd][Ee][Nn][Tt][Ii][Aa][Ll])/

const REDACT: Array<{ re: RegExp; repl: string; scan: 're' | 'keyword' | 'bearer' | 'jwt' | 'aws' | 'token' }> = [
  { re: /-----BEGIN [A-Z ]*PRIVATE KEY-----[\0-\uFFFF]*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)/g, repl: '[redacted private key]', scan: 're' },
  { re: /([Bb][Ee][Aa][Rr][Ee][Rr]|[Bb][Aa][Ss\u017F][Ii][Cc])[ \t\n\r\f]+[A-Za-z\u017F\u212A0-9._~+/=-]{8,}/g, repl: '$1 [redacted]', scan: 'bearer' },
  { re: /(?:)/, repl: '$1$2[redacted]', scan: 'keyword' },
  { re: /eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}/g, repl: '[redacted jwt]', scan: 'jwt' },
  { re: /(?:AKIA|ASIA)[0-9A-Z]{16}/g, repl: '[redacted aws key]', scan: 'aws' },
  { re: /(?:gh[pousr]_[A-Za-z0-9]{20,}[A-Za-z0-9_]*|glpat-[A-Za-z0-9_-]{20,}|xox[abpr]-[A-Za-z0-9-]{10,}[A-Za-z0-9_]*)/g, repl: '[redacted token]', scan: 'token' },
  { re: /:\/\/([^/ \t\n\r\f:@]+):([^@ \t\n\r\f/]+)@/g, repl: '://$1:[redacted]@', scan: 're' },
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

// replaceLinear keeps overlapping starts. A failed edge resumes one character
// later. next walks forward only, so a long token tail is not re-scanned.
function replaceLinear(
  s: string,
  next: (from: number) => { start: number; end: number; repl: string } | null,
): string {
  let out = ''
  let i = 0
  while (i < s.length) {
    const m = next(i)
    if (!m || m.start < i || m.end <= m.start) {
      out += s.slice(i)
      break
    }
    if (leftEdge(s, m.start) && rightEdge(s, m.end)) {
      out += s.slice(i, m.start) + m.repl
      i = m.end
      continue
    }
    let n = m.start + 1
    if (n <= i) n = i + 1
    if (n > s.length) {
      out += s.slice(i)
      break
    }
    out += s.slice(i, n)
    i = n
  }
  return out
}

function count(steps: { n: number } | null, k: number): void {
  if (steps && k > 0) steps.n += k
}

function foldCode(c: number, lower: number): boolean {
  return c === lower || c === lower - 32
}

function bearerSecret(c: number): boolean {
  return c === 46 || c === 95 || c === 126 || c === 43 || c === 47 || c === 61 || c === 45 ||
    (c >= 48 && c <= 57) || (c >= 65 && c <= 90) || (c >= 97 && c <= 122) ||
    c === 0x17f || c === 0x212a
}

function buildBearerRuns(s: string, steps: { n: number } | null): Array<{ runes: number; end: number }> {
  const r = new Array<{ runes: number; end: number }>(s.length + 1)
  r[s.length] = { runes: 0, end: s.length }
  for (let i = s.length - 1; i >= 0; i--) {
    count(steps, 1)
    const c = s.charCodeAt(i)
    if (!bearerSecret(c)) {
      r[i] = { runes: 0, end: i }
      continue
    }
    const next = r[i + 1]
    if (next && next.runes > 0) r[i] = { runes: 1 + next.runes, end: next.end }
    else r[i] = { runes: 1, end: i + 1 }
  }
  return r
}

function matchScheme(s: string, i: number): number {
  if (i + 6 <= s.length &&
    foldCode(s.charCodeAt(i), 98) && foldCode(s.charCodeAt(i + 1), 101) && foldCode(s.charCodeAt(i + 2), 97) &&
    foldCode(s.charCodeAt(i + 3), 114) && foldCode(s.charCodeAt(i + 4), 101) && foldCode(s.charCodeAt(i + 5), 114)) {
    return i + 6
  }
  if (i >= s.length || !foldCode(s.charCodeAt(i), 98) || !foldCode(s.charCodeAt(i + 1), 97)) return -1
  let j = i + 2
  const sCode = s.charCodeAt(j)
  if (sCode === 115 || sCode === 83) j++
  else if (sCode === 0x17f) j++
  else return -1
  if (j + 2 <= s.length && foldCode(s.charCodeAt(j), 105) && foldCode(s.charCodeAt(j + 1), 99)) return j + 2
  return -1
}

function wsLen(s: string, i: number): number {
  let j = i
  while (j < s.length) {
    const c = s.charCodeAt(j)
    if (c !== 32 && c !== 9 && c !== 10 && c !== 13 && c !== 12) break
    j++
  }
  return j - i
}

function replaceBearer(s: string, steps: { n: number } | null): string {
  const runs = buildBearerRuns(s, steps)
  return replaceLinear(s, (from) => {
    for (let j = from; j < s.length; j++) {
      count(steps, 1)
      const schemeEnd = matchScheme(s, j)
      if (schemeEnd < 0) continue
      const ws = wsLen(s, schemeEnd)
      count(steps, ws)
      if (ws < 1) continue
      const val = schemeEnd + ws
      const run = runs[val]
      if (!run || run.runes < 8) continue
      return { start: j, end: run.end, repl: s.slice(j, schemeEnd) + ' [redacted]' }
    }
    return null
  })
}

function buildClassRun(s: string, steps: { n: number } | null, ok: (c: number) => boolean): number[] {
  const run = new Array<number>(s.length + 1).fill(0)
  for (let i = s.length - 1; i >= 0; i--) {
    count(steps, 1)
    if (ok(s.charCodeAt(i))) run[i] = run[i + 1] + 1
  }
  return run
}

function jwtByte(c: number): boolean {
  return (c >= 48 && c <= 57) || (c >= 65 && c <= 90) || (c >= 97 && c <= 122) || c === 95 || c === 45
}

function jwtAt(s: string, i: number, run: number[]): number {
  if (i + 3 > s.length || s[i] !== 'e' || s[i + 1] !== 'y' || s[i + 2] !== 'J') return -1
  let p = i + 3
  if (p > s.length || run[p] < 8) return -1
  p += run[p]
  if (p >= s.length || s[p] !== '.') return -1
  p++
  if (p > s.length || run[p] < 8) return -1
  p += run[p]
  if (p >= s.length || s[p] !== '.') return -1
  p++
  if (p > s.length || run[p] < 8) return -1
  return p + run[p]
}

function replaceJWT(s: string, steps: { n: number } | null): string {
  const run = buildClassRun(s, steps, jwtByte)
  return replaceLinear(s, (from) => {
    for (let j = from; j < s.length; j++) {
      count(steps, 1)
      const end = jwtAt(s, j, run)
      if (end >= 0) return { start: j, end, repl: '[redacted jwt]' }
    }
    return null
  })
}

function awsAt(s: string, i: number, steps: { n: number } | null): number {
  if (i + 20 > s.length) return -1
  const prefix = s.slice(i, i + 4)
  if (prefix !== 'AKIA' && prefix !== 'ASIA') return -1
  for (let k = i + 4; k < i + 20; k++) {
    count(steps, 1)
    const c = s.charCodeAt(k)
    if (c < 48 || c > 90 || (c > 57 && c < 65)) return -1
  }
  return i + 20
}

function replaceAWS(s: string, steps: { n: number } | null): string {
  return replaceLinear(s, (from) => {
    for (let j = from; j < s.length; j++) {
      count(steps, 1)
      const end = awsAt(s, j, steps)
      if (end >= 0) return { start: j, end, repl: '[redacted aws key]' }
    }
    return null
  })
}

function isAZ09(c: number): boolean {
  return (c >= 48 && c <= 57) || (c >= 65 && c <= 90) || (c >= 97 && c <= 122)
}

function buildTokenRuns(s: string, steps: { n: number } | null): { alnum: number[]; alnumU: number[]; alnumH: number[]; alnumUH: number[] } {
  const n = s.length
  const alnum = new Array<number>(n + 1).fill(0)
  const alnumU = new Array<number>(n + 1).fill(0)
  const alnumH = new Array<number>(n + 1).fill(0)
  const alnumUH = new Array<number>(n + 1).fill(0)
  for (let i = n - 1; i >= 0; i--) {
    count(steps, 1)
    const c = s.charCodeAt(i)
    const az = isAZ09(c)
    if (az) alnum[i] = alnum[i + 1] + 1
    if (az || c === 95) alnumU[i] = alnumU[i + 1] + 1
    if (az || c === 45) alnumH[i] = alnumH[i + 1] + 1
    if (az || c === 95 || c === 45) alnumUH[i] = alnumUH[i + 1] + 1
  }
  return { alnum, alnumU, alnumH, alnumUH }
}

function tokenAt(s: string, i: number, r: { alnum: number[]; alnumU: number[]; alnumH: number[]; alnumUH: number[] }): number {
  if (i + 4 <= s.length && s[i] === 'g' && s[i + 1] === 'h' && s[i + 3] === '_') {
    const k = s[i + 2]
    if (k === 'p' || k === 'o' || k === 'u' || k === 's' || k === 'r') {
      const n = r.alnum[i + 4]
      if (n >= 20) {
        let e = i + 4 + n
        if (e < s.length) e += r.alnumU[e]
        return e
      }
    }
  }
  if (i + 6 <= s.length && s.slice(i, i + 6) === 'glpat-') {
    const n = r.alnumUH[i + 6]
    if (n >= 20) return i + 6 + n
  }
  if (i + 5 <= s.length && s[i] === 'x' && s[i + 1] === 'o' && s[i + 2] === 'x' && s[i + 4] === '-') {
    const k = s[i + 3]
    if (k === 'a' || k === 'b' || k === 'p' || k === 'r') {
      const n = r.alnumH[i + 5]
      if (n >= 10) {
        let e = i + 5 + n
        if (e < s.length) e += r.alnumU[e]
        return e
      }
    }
  }
  return -1
}

function replaceToken(s: string, steps: { n: number } | null): string {
  const runs = buildTokenRuns(s, steps)
  return replaceLinear(s, (from) => {
    for (let j = from; j < s.length; j++) {
      count(steps, 1)
      const end = tokenAt(s, j, runs)
      if (end >= 0) return { start: j, end, repl: '[redacted token]' }
    }
    return null
  })
}

function isASCIIWS(c: number): boolean {
  return c === 32 || c === 9 || c === 10 || c === 13 || c === 12
}

function isValueStop(c: number): boolean {
  // Controls, bidi marks and zero-width characters stop a value. If one of
  // them were the whole value, redacting it would consume the assignment and
  // a secret after the strip would remain.
  if (isASCIIWS(c) || c === 34 || c === 39 || c === 44 || c === 59) return true
  if (c === 0x2028 || c === 0x2029 || BIDI.has(c) || ZERO.has(c)) return true
  return c < 0x20 || (c >= 0x7f && c <= 0x9f)
}

function isSecretClass(c: number): boolean {
  return c === 95 || c === 46 || c === 45 ||
    (c >= 48 && c <= 57) || (c >= 65 && c <= 90) || (c >= 97 && c <= 122) ||
    c === 0x17f || c === 0x212a
}

// replaceKeyword masks keyword assignments in one pass. '=' or ':' is required,
// so a run of one character is a single walk. A separator that is not an
// assignment is stepped past; the search does not restart one character later.
function replaceKeyword(s: string, steps: { n: number } | null): string {
  let out = ''
  let i = 0
  while (i < s.length) {
    let sep = -1
    for (let k = i; k < s.length; k++) {
      if (steps) steps.n++
      const c = s.charCodeAt(k)
      if (c === 61 || c === 58) {
        sep = k
        break
      }
    }
    if (sep < 0) {
      out += s.slice(i)
      break
    }
    const parsed = keywordAssignmentAt(s, i, sep, steps)
    if (!parsed || !leftEdge(s, parsed.start) || !rightEdge(s, parsed.valEnd)) {
      out += s.slice(i, sep + 1)
      i = sep + 1
      continue
    }
    out += s.slice(i, parsed.start) + s.slice(parsed.start, parsed.sepEnd) + '[redacted]'
    i = parsed.valEnd
  }
  return out
}

function keywordAssignmentAt(s: string, from: number, sep: number, steps: { n: number } | null): { start: number; sepEnd: number; valEnd: number } | null {
  let j = sep
  while (j > 0) {
    const c = s.charCodeAt(j - 1)
    if (steps) steps.n++
    if (!isASCIIWS(c)) break
    j--
  }
  if (j > 0) {
    const c = s.charCodeAt(j - 1)
    if (c === 39 || c === 34) {
      if (steps) steps.n++
      j--
    }
  }
  const idEnd = j
  let idStart = idEnd
  while (idStart > 0) {
    const c = s.charCodeAt(idStart - 1)
    if (steps) steps.n++
    if (!isSecretClass(c)) break
    idStart--
  }
  if (idStart < from) idStart = from
  if (idStart >= idEnd || !KEYWORD_HIT.test(s.slice(idStart, idEnd))) return null
  let p = sep + 1
  while (p < s.length) {
    const c = s.charCodeAt(p)
    if (steps) steps.n++
    if (!isASCIIWS(c)) break
    p++
  }
  if (p < s.length) {
    const c = s.charCodeAt(p)
    if (c === 39 || c === 34) {
      if (steps) steps.n++
      p++
    }
  }
  const valStart = p
  let valEnd = valStart
  while (valEnd < s.length) {
    const c = s.charCodeAt(valEnd)
    if (steps) steps.n++
    if (isValueStop(c)) break
    valEnd++
  }
  if (valEnd === valStart) return null
  return { start: idStart, sepEnd: valStart, valEnd }
}

function applyRedact(s: string, steps: { n: number } | null): string {
  let out = s
  for (const rule of REDACT) {
    switch (rule.scan) {
      case 'keyword':
        out = replaceKeyword(out, steps)
        break
      case 'bearer':
        out = replaceBearer(out, steps)
        break
      case 'jwt':
        out = replaceJWT(out, steps)
        break
      case 'aws':
        out = replaceAWS(out, steps)
        break
      case 'token':
        out = replaceToken(out, steps)
        break
      default:
        out = out.replace(rule.re, rule.repl)
    }
  }
  return out
}

/** Masks the same secret shapes as api/internal/notice.Redact. */
export function redact(s: string): string {
  return applyRedact(s, null)
}

/** Characters the scans examine, including class-run walks. Linear in the length. */
export function redactScanSteps(s: string): number {
  const steps = { n: 0 }
  applyRedact(s, steps)
  return steps.n
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
