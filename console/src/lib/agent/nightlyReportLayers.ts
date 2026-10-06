/**
 * Nightly agent report (agent host drift scan) — per-layer pass/fail parsed
 * from the markdown report returned by GET /api/v1/agent/nightly-report.
 */

function extractReportSection(content: string, marker: string): string {
  const idx = content.indexOf(marker)
  if (idx < 0) return ''
  const rest = content.slice(idx + marker.length)
  const next = rest.search(/\n## /)
  return next < 0 ? rest : rest.slice(0, next)
}

function layer4Section(content: string): string {
  const start = content.indexOf('## Layer 4')
  if (start < 0) return ''
  const end = content.indexOf('## Runner health', start)
  return end > start ? content.slice(start, end) : content.slice(start, start + 8000)
}

export function parseNightlyLayerResults(content: string | undefined): {
  l1: 'pass' | 'fail' | 'unknown'
  l2: 'pass' | 'fail' | 'unknown'
  l3: 'pass' | 'fail' | 'unknown'
  l4Hint: 'no_drift' | 'posted' | 'post_skipped' | 'post_failed' | 'unknown'
} {
  if (content == null || content.trim() === '') {
    return { l1: 'unknown', l2: 'unknown', l3: 'unknown', l4Hint: 'unknown' }
  }

  const l1 = extractReportSection(content, '## Layer 1 — Catalog drift scan') || extractReportSection(content, '## Layer 1')
  const l2 = extractReportSection(content, '## Layer 2 — API probe scan') || extractReportSection(content, '## Layer 2')
  const l3 = extractReportSection(content, '## Layer 3 — Semantic / spine drift scan') || extractReportSection(content, '## Layer 3')

  const layerStatus = (section: string, passPhrases: string[]): 'pass' | 'fail' | 'unknown' => {
    if (section.trim() === '') return 'unknown'
    if (/Findings:\s*[1-9]\d*/.test(section)) return 'fail'
    if (/### Failures/.test(section)) return 'fail'
    if (passPhrases.some(p => section.includes(p))) return 'pass'
    if (/Findings:\s*0/.test(section)) return 'pass'
    return 'unknown'
  }

  let l4Hint: 'no_drift' | 'posted' | 'post_skipped' | 'post_failed' | 'unknown' = 'unknown'
  const l4Block = layer4Section(content)
  if (l4Block.includes('No drift — skipping Layer 4 proposal') || content.includes('No drift — skipping Layer 4 proposal')) {
    l4Hint = 'no_drift'
  } else if (l4Block.includes('SKIP proposal POST')) {
    l4Hint = 'post_skipped'
  } else if (l4Block.includes('POST failed')) {
    l4Hint = 'post_failed'
  } else if (l4Block.includes('Owner approval: Ops Console')) {
    l4Hint = 'posted'
  }

  return {
    l1: layerStatus(l1, ['No deterministic drift detected.']),
    l2: layerStatus(l2, ['All API probes passed.']),
    l3: layerStatus(l3, ['Live spine matches static catalog authorities.']),
    l4Hint,
  }
}
