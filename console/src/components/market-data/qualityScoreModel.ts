import type { QualityCheckItem } from '@/api/marketDataPlugin'

export function qualityCheckFill(item: QualityCheckItem): number {
  if (item.ok) return 100
  const d = item.detail ?? ''
  const missing = d.match(/missing\s+(\d+)\s*\/\s*(\d+)/i)
  if (missing) {
    const miss = Number(missing[1])
    const total = Number(missing[2])
    if (total > 0) return Math.max(0, ((total - miss) / total) * 100)
  }
  const covered = d.match(/(\d+)\s*\/\s*(\d+)\s+(optionable|symbols|covered)/i)
  if (covered) {
    const n = Number(covered[1])
    const total = Number(covered[2])
    if (total > 0) return Math.max(0, (n / total) * 100)
  }
  const gaps = d.match(/(\d+)\s+gaps?\s+over\s+(\d+)/i)
  if (gaps) {
    const g = Number(gaps[1])
    const days = Number(gaps[2])
    if (days > 0) return Math.max(0, Math.min(100, (1 - g / days) * 100))
  }
  return 0
}

export function qualityCheckLabel(check: string): string {
  if (check === 'stock_daily_coverage') return 'Stock daily'
  if (check === 'option_snapshot_coverage') return 'Option snapshot'
  if (check === 'option_oi_coverage') return 'Option OI'
  if (check === 'freshness') return 'Freshness'
  return check.replace(/_/g, ' ')
}

/** Compact caption for DashCard — keeps the fail reason, drops the long raw clause. */
export function qualityCheckCaption(item: QualityCheckItem): string {
  const d = (item.detail ?? '').trim()
  if (d === '' || /^ok$/i.test(d)) return item.ok ? 'Fresh' : d

  const parts: string[] = []
  const symbols = d.match(/symbols\s*=\s*([\d,]+)/i)
  if (symbols) parts.push(`${Number(symbols[1].replace(/,/g, '')).toLocaleString('en-US')} sym`)

  const need = d.match(/need\s*>\s*([\d,]+)/i)
  if (need) parts.push(`need >${need[1]}`)

  const gapsEq = d.match(/gaps\s*=\s*([\d,]+)\s+over\s+([\d,]+)/i)
  const gapsPlain = d.match(/([\d,]+)\s+gaps?\s+over\s+([\d,]+)/i)
  const gaps = gapsEq ?? gapsPlain
  if (gaps) parts.push(`${gaps[1]} gaps / ${gaps[2]}d`)

  const times = d.match(/[×x]\s*([\d,]+)\s+(watchlist|optionable)/i)
  if (times) parts.push(`× ${times[1]}`)

  const missingEq = d.match(/missing\s*=\s*([\d,]+)\s*\/\s*([\d,]+)/i)
  const missingPlain = d.match(/missing\s+([\d,]+)\s*\/\s*([\d,]+)/i)
  const missing = missingEq ?? missingPlain
  if (missing) parts.push(`miss ${missing[1]}/${missing[2]}`)

  const skipped = d.match(/skipped\s+([\d,]+)/i)
  if (skipped && Number(skipped[1]) > 0) parts.push(`skip ${skipped[1]} eq`)

  const target = d.match(/target\s*=\s*(\d{4}-\d{2}-\d{2})/i)
  if (target) parts.push(target[1].slice(5))

  if (parts.length > 0) return parts.join(' · ')

  if (item.check === 'freshness' && !item.ok) {
    return d
      .split(';')
      .map(s => s.trim())
      .filter(Boolean)
      .slice(0, 2)
      .map(s => s.replace(/age_hours=([0-9.]+)/, (_, n) => `${Number(n).toFixed(0)}h`))
      .join(' · ')
  }
  return d
}
