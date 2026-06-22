export type StaticLogLevel = 'ERROR' | 'WARN' | 'INFO' | 'DEBUG' | 'OTHER'

export type StaticLogLevelFilter = StaticLogLevel | 'ALL' | 'ALERTS'

export interface StaticLogLine {
  id: number
  ts: string
  level: StaticLogLevel
  message: string
}

export const STATIC_LOG_LEVEL_LABEL: Record<StaticLogLevel, string> = {
  ERROR: 'ERR',
  WARN: 'WARN',
  INFO: 'INFO',
  DEBUG: 'DBG',
  OTHER: '···',
}

export const STATIC_LOG_LEVEL_FILTER_OPTIONS: StaticLogLevelFilter[] = [
  'ALERTS',
  'ALL',
  'ERROR',
  'WARN',
  'INFO',
  'DEBUG',
]

const LINE_RE =
  /^(\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2})(?:[,.]\d+)?\s+\[?(ERROR|WARN(?:ING)?|INFO|DEBUG)\]?\s+(.*)/i

function normalizeLevel(raw: string): StaticLogLevel {
  const u = raw.toUpperCase()
  if (u === 'ERROR') return 'ERROR'
  if (u === 'WARNING' || u === 'WARN') return 'WARN'
  if (u === 'INFO') return 'INFO'
  if (u === 'DEBUG') return 'DEBUG'
  return 'OTHER'
}

export function parseStaticLogText(text: string): StaticLogLine[] {
  const lines = text.split(/\r?\n/).filter(line => line.length > 0)
  return lines.map((raw, index) => {
    const m = raw.match(LINE_RE)
    if (m != null) {
      return {
        id: index,
        ts: m[1].includes('T') ? m[1].slice(11, 19) : m[1].slice(11),
        level: normalizeLevel(m[2]),
        message: m[3].trim(),
      }
    }
    const bracket = raw.match(/^\[?(ERROR|WARN(?:ING)?|INFO|DEBUG)\]?\s+(.*)/i)
    if (bracket != null) {
      return {
        id: index,
        ts: '',
        level: normalizeLevel(bracket[1]),
        message: bracket[2].trim(),
      }
    }
    return {
      id: index,
      ts: '',
      level: 'OTHER',
      message: raw,
    }
  })
}

export function staticLogMatchesFilter(level: StaticLogLevel, filter: StaticLogLevelFilter): boolean {
  if (filter === 'ALL') return true
  if (filter === 'ALERTS') return level === 'ERROR' || level === 'WARN'
  return level === filter
}

export function filterStaticLogLines(
  lines: StaticLogLine[],
  filter: StaticLogLevelFilter,
  search: string,
): StaticLogLine[] {
  const q = search.trim().toLowerCase()
  return lines.filter(line => {
    if (!staticLogMatchesFilter(line.level, filter)) return false
    if (q === '') return true
    return (
      line.message.toLowerCase().includes(q) ||
      line.ts.toLowerCase().includes(q) ||
      line.level.toLowerCase().includes(q)
    )
  })
}

export function staticLogFilterLabel(filter: StaticLogLevelFilter): string {
  if (filter === 'ALL') return 'All'
  if (filter === 'ALERTS') return 'Err · Warn'
  return STATIC_LOG_LEVEL_LABEL[filter]
}
