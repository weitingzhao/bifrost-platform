import type { ComponentLineId, WorkTrackType } from '@/lib/briefing/briefingViewTabs'
import { componentLineById, trackTypeById } from '@/lib/briefing/briefingViewTabs'

export type LaneClassifyConfidence = 'high' | 'medium' | 'low'

export type LaneClassifyInput = {
  label: string
  description: string
}

export type LaneClassifyResult = {
  line: ComponentLineId
  trackType: WorkTrackType
  confidence: LaneClassifyConfidence
  reason: string
}

type KeywordRule = {
  line: ComponentLineId
  patterns: RegExp[]
  weight: number
}

const LINE_RULES: KeywordRule[] = [
  {
    line: 'rocket',
    weight: 3,
    patterns: [
      /\bconsole\b/i,
      /\bcluster\b/i,
      /\bgitops\b/i,
      /\bci\s*\/?\s*cd\b/i,
      /\bplatform(?:-?api)?\b/i,
      /\bmcp\b/i,
      /\bops\s*platform\b/i,
      /\bbriefing\b/i,
    ],
  },
  {
    line: 'satellite',
    weight: 3,
    patterns: [
      /\btrade\b/i,
      /\bk8s[- ]?native\b/i,
      /\bdaemon\b/i,
      /\bmigration\b/i,
      /\blegacy\b/i,
      /\bbi[-\s]?frost[- ]?trade\b/i,
      /\bfrontend\b/i,
      /\bdata\s*layer\b/i,
    ],
  },
  {
    line: 'engineer',
    weight: 3,
    patterns: [
      /\bagent\b/i,
      /\bdrift\b/i,
      /\bautomation\b/i,
      /\bplugin\b/i,
      /\bhermes\b/i,
      /\bremediation\b/i,
      /\bnous\b/i,
    ],
  },
  {
    line: 'ground',
    weight: 3,
    patterns: [
      /\bserver\b/i,
      /\blan\b/i,
      /\bwifi\b/i,
      /\bnetwork\b/i,
      /\bhardware\b/i,
      /\bwake[- ]?on[- ]?lan\b/i,
    ],
  },
  {
    line: 'operations',
    weight: 2,
    patterns: [
      /\bcelery\b/i,
      /\bqueue\b/i,
      /\bgovernance\b/i,
      /\bdebug\b/i,
      /\brelease\b/i,
      /\bday[- ]?to[- ]?day\b/i,
      /\btroubleshoot/i,
      /\bops\b/i,
    ],
  },
  {
    line: 'subcontractor',
    weight: 3,
    patterns: [
      /\bpolygon\b/i,
      /\binteractive\s+brokers\b/i,
      /\bib\b/i,
      /\bvendor\b/i,
      /\bsla\b/i,
      /\bsubcontractor\b/i,
    ],
  },
]

type TrackRule = {
  trackType: WorkTrackType
  patterns: RegExp[]
}

const TRACK_RULES: TrackRule[] = [
  {
    trackType: 'migrate',
    patterns: [/\bmigrate\b/i, /\bmigration\b/i, /\blegacy\b/i, /\brefactor\b/i],
  },
  {
    trackType: 'maintain',
    patterns: [
      /\bmonitor\b/i,
      /\bhealth\b/i,
      /\btroubleshoot/i,
      /\bmaintain\b/i,
      /\bgovernance\b/i,
    ],
  },
  {
    trackType: 'release',
    patterns: [/\bdeploy\b/i, /\brelease\b/i, /\bpromote\b/i, /\bcutover\b/i],
  },
]

function scoreLine(text: string): { line: ComponentLineId; score: number; hits: string[] }[] {
  return LINE_RULES.map(rule => {
    const hits: string[] = []
    let score = 0
    for (const re of rule.patterns) {
      const m = text.match(re)
      if (m != null) {
        hits.push(m[0])
        score += rule.weight
      }
    }
    return { line: rule.line, score, hits }
  })
    .filter(r => r.score > 0)
    .sort((a, b) => b.score - a.score)
}

function classifyTrackType(text: string): { trackType: WorkTrackType; hits: string[] } {
  for (const rule of TRACK_RULES) {
    const hits: string[] = []
    for (const re of rule.patterns) {
      const m = text.match(re)
      if (m != null) hits.push(m[0])
    }
    if (hits.length > 0) {
      return { trackType: rule.trackType, hits }
    }
  }
  return { trackType: 'build', hits: [] }
}

/**
 * Rule-based classifier for New Lane — describe first, then confirm Line / Track Type.
 */
export function classifyLane(input: LaneClassifyInput): LaneClassifyResult {
  const text = `${input.label}\n${input.description}`.trim()
  if (text === '') {
    return {
      line: 'rocket',
      trackType: 'build',
      confidence: 'low',
      reason: 'No description yet — defaulting to Rocket · Build. Please choose manually.',
    }
  }

  const scored = scoreLine(text)
  const track = classifyTrackType(text)

  if (scored.length === 0) {
    return {
      line: 'rocket',
      trackType: track.trackType,
      confidence: 'low',
      reason: 'Could not infer component line from keywords — please select manually.',
    }
  }

  const top = scored[0]
  const second = scored[1]
  const lineLabel = componentLineById(top.line).shortLabel
  const ttLabel = trackTypeById(track.trackType).shortLabel
  const hitPreview = top.hits.slice(0, 3).map(h => `"${h}"`).join(', ')

  let confidence: LaneClassifyConfidence = 'medium'
  if (top.score >= 6 && (second == null || top.score - second.score >= 3)) {
    confidence = 'high'
  } else if (second != null && top.score - second.score < 2) {
    confidence = 'low'
  }

  const trackNote =
    track.hits.length > 0
      ? ` Track Type → ${ttLabel} (matched ${track.hits
          .slice(0, 2)
          .map(h => `"${h}"`)
          .join(', ')}).`
      : ` Track Type → ${ttLabel} (default).`

  return {
    line: top.line,
    trackType: track.trackType,
    confidence,
    reason: `Matched ${hitPreview} → ${lineLabel}.${trackNote}`,
  }
}
