import type { StatusWord } from '@/lib/shell/stateWords'
import type { PatrolRunResult, PatrolSkill } from './readings'

/**
 * Records → Patrol's head line, from the skills read (箭10 2026-11-23, the inventory's "false OK": the page drew an
 * ok lamp that nothing had checked). The word is the worst of the enabled skills' last results, in the engine's
 * seven words; a skill that never ran or was skipped cannot be judged, so it is unknown, never ok.
 */
const RANK: StatusWord[] = ['fail', 'warn', 'unknown', 'pending', 'ok']

function wordOfResult(r: PatrolRunResult | undefined): StatusWord {
  if (r === 'success') return 'ok'
  if (r === 'failure') return 'fail'
  if (r === 'escalated') return 'warn'
  if (r === 'running') return 'pending'
  return 'unknown'
}

export type PatrolVerdict = { word: StatusWord; sentence: string }

export function patrolVerdict(skills: readonly PatrolSkill[] | undefined, loading: boolean, error: string | null): PatrolVerdict {
  if (error != null) return { word: 'unknown', sentence: `Skills could not be read: ${error}` }
  if (loading || skills == null) return { word: 'pending', sentence: 'Reading the skills…' }
  const enabled = skills.filter(s => s.enabled)
  if (enabled.length === 0) return { word: 'boundary', sentence: `No skill is enabled (${skills.length} loaded)` }
  const counts = new Map<string, number>()
  let word: StatusWord = 'ok'
  for (const s of enabled) {
    const w = wordOfResult(s.last_result)
    if (RANK.indexOf(w) < RANK.indexOf(word)) word = w
    const key = s.last_result ?? 'never run'
    counts.set(key, (counts.get(key) ?? 0) + 1)
  }
  const parts = [...counts.entries()].map(([k, n]) => `${n} ${k}`)
  return { word, sentence: `${enabled.length} enabled, last runs: ${parts.join(' · ')}` }
}
