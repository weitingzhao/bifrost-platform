/** Phase → Tekton pipelineTask names (mirrors api/internal/delivery/pipeline_steps.go). */

const PLATFORM_PHASE_TASKS: Record<string, string[]> = {
  mirror: ['mirror-sync'],
  clone: ['clone-platform', 'clone-ui'],
  build: [
    'stage-api-dockerfile',
    'stage-console-dockerfile',
    'build-platform-api',
    'build-platform-console',
  ],
  rollout: ['rollout'],
  gitops: ['gitops-sync'],
}

const PLATFORM_PROD_PHASE_TASKS: Record<string, string[]> = {
  preflight: ['preflight-stg'],
  ...PLATFORM_PHASE_TASKS,
}

const STG_PHASE_TASKS: Record<string, string[]> = {
  clone: [
    'clone-core',
    'clone-worker',
    'clone-socket',
    'clone-api',
    'clone-frontend',
    'clone-ui',
    'clone-infra',
  ],
  prepare: ['prepare'],
  build: [
    'stage-api-dockerfile',
    'build-all-apis',
    'stage-frontend-dockerfile',
    'build-frontend',
    'build-worker-socket',
  ],
  rollout: ['rollout'],
  verify: ['verify-stg'],
  gitops: ['gitops-sync'],
}

export function phaseTasksForPipeline(
  pipeline: string,
  phaseId: string,
): string[] {
  const map =
    pipeline === 'bifrost-deliver-platform-prod'
      ? PLATFORM_PROD_PHASE_TASKS
      : pipeline === 'bifrost-deliver-platform'
        ? PLATFORM_PHASE_TASKS
        : STG_PHASE_TASKS
  return map[phaseId] ?? []
}

export interface LogSection {
  header: string
  body: string
}

/** Split Tekton log bundle on `=== pod/container` headers. */
export function splitPipelineLogSections(logs: string): LogSection[] {
  if (logs === '') return []
  const parts = logs.split(/(?=^=== )/m)
  const out: LogSection[] = []
  for (const part of parts) {
    const trimmed = part.replace(/^\n+/, '')
    if (trimmed === '') continue
    const nl = trimmed.indexOf('\n')
    if (nl < 0) {
      out.push({ header: trimmed, body: '' })
      continue
    }
    out.push({ header: trimmed.slice(0, nl), body: trimmed.slice(nl + 1) })
  }
  return out
}

function headerMatchesTask(header: string, task: string): boolean {
  const h = header.toLowerCase()
  const t = task.toLowerCase()
  return (
    h.includes(`-${t}-`) ||
    h.includes(`-${t}-pod`) ||
    h.includes(`-${t}/`) ||
    h.endsWith(`-${t}`) ||
    h.includes(`/${t}`)
  )
}

/**
 * Keep only sections whose pod/container header matches the phase's Tekton tasks.
 * Returns null when phaseId is null/empty (show all).
 */
export function filterLogsByPhase(
  logs: string,
  phaseId: string | null,
  pipeline: string,
): string | null {
  if (phaseId == null || phaseId === '') return null
  const tasks = phaseTasksForPipeline(pipeline, phaseId)
  if (tasks.length === 0) return null
  const sections = splitPipelineLogSections(logs)
  if (sections.length === 0) return null
  const matched = sections.filter(s =>
    tasks.some(task => headerMatchesTask(s.header, task)),
  )
  if (matched.length === 0) {
    return `(no log sections for phase "${phaseId}" in this run yet)`
  }
  return matched.map(s => (s.body !== '' ? `${s.header}\n${s.body}` : s.header)).join('')
}

export function formatSecondsAgo(seconds: number): string {
  if (seconds < 0) return '0s ago'
  if (seconds < 60) return `${seconds}s ago`
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  if (m < 60) return s === 0 ? `${m}m ago` : `${m}m ${s}s ago`
  const h = Math.floor(m / 60)
  const rm = m % 60
  return rm === 0 ? `${h}h ago` : `${h}h ${rm}m ago`
}

export function secondsSince(isoOrMs: string | number | null | undefined, nowMs: number): number | null {
  if (isoOrMs == null || isoOrMs === '') return null
  const t = typeof isoOrMs === 'number' ? isoOrMs : Date.parse(isoOrMs)
  if (Number.isNaN(t)) return null
  return Math.max(0, Math.floor((nowMs - t) / 1000))
}

export function isPipelineLogPlaceholder(logs: string): boolean {
  const t = logs.trim()
  return (
    t === '' ||
    t.includes('no pods yet') ||
    t.includes('no log lines yet') ||
    t === '(empty)'
  )
}

function normalizeSectionHeader(header: string): string {
  return header.replace(/\s+/g, ' ').trim()
}

function findLineSubarray(haystack: string[], needle: string[]): number {
  if (needle.length === 0 || needle.length > haystack.length) return -1
  outer: for (let i = 0; i <= haystack.length - needle.length; i++) {
    for (let j = 0; j < needle.length; j++) {
      if (haystack[i + j] !== needle[j]) continue outer
    }
    return i
  }
  return -1
}

/** Append-only merge for one container body across sliding kubectl tails. */
export function mergeSectionBody(prevBody: string, nextBody: string): string {
  if (nextBody === '') return prevBody
  if (prevBody === '') return nextBody
  if (nextBody === prevBody) return prevBody
  if (nextBody.startsWith(prevBody)) return nextBody
  if (prevBody.startsWith(nextBody)) return prevBody

  const prevLines = prevBody.replace(/\n$/, '').split('\n')
  const nextLines = nextBody.replace(/\n$/, '').split('\n')
  const maxNeedle = Math.min(48, prevLines.length, nextLines.length)
  for (let n = maxNeedle; n >= 1; n--) {
    const needle = prevLines.slice(-n)
    const at = findLineSubarray(nextLines, needle)
    if (at < 0) continue
    const after = nextLines.slice(at + n)
    if (after.length === 0) return prevBody.endsWith('\n') ? prevBody : `${prevBody}\n`
    const base = prevBody.replace(/\n$/, '')
    return `${base}\n${after.join('\n')}\n`
  }
  // No overlap (container restarted / different stream) — keep longer, prefer next if similar size.
  if (nextBody.length >= prevBody.length) return nextBody
  return prevBody
}

function joinSections(sections: LogSection[]): string {
  return sections
    .map(s => (s.body !== '' ? `${s.header}\n${s.body}` : s.header))
    .join('')
}

/**
 * Merge successive log API snapshots into an append-only transcript.
 * Prevents TailLines sliding windows from rewriting earlier lines every poll.
 */
export function mergePipelineLogSnapshots(prev: string, next: string): string {
  if (isPipelineLogPlaceholder(next)) return prev === '' ? next : prev
  if (prev === '' || isPipelineLogPlaceholder(prev)) return next
  if (prev === next) return prev
  if (next.startsWith(prev)) return next

  const prevSections = splitPipelineLogSections(prev)
  const nextSections = splitPipelineLogSections(next)
  if (nextSections.length === 0) return prev
  if (prevSections.length === 0) return next

  const bodies = new Map<string, string>()
  const headers = new Map<string, string>()
  const order: string[] = []

  for (const s of prevSections) {
    const key = normalizeSectionHeader(s.header)
    if (!bodies.has(key)) order.push(key)
    headers.set(key, s.header)
    bodies.set(key, s.body)
  }
  for (const s of nextSections) {
    const key = normalizeSectionHeader(s.header)
    if (!bodies.has(key)) order.push(key)
    headers.set(key, s.header)
    bodies.set(key, mergeSectionBody(bodies.get(key) ?? '', s.body))
  }

  return joinSections(
    order.map(key => ({
      header: headers.get(key) ?? key,
      body: bodies.get(key) ?? '',
    })),
  )
}
