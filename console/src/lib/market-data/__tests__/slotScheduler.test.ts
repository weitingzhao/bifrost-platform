import { readdirSync, readFileSync, statSync } from 'node:fs'
import path from 'node:path'
import { describe, expect, it } from 'vitest'
import {
  buildSlotScheduleIndex,
  dagsterScheduleForSlot,
  slotSchedulerKind,
  slotSchedulerLabel,
} from '@/lib/market-data/slotScheduler'
import statusFixture from './fixtures/research-orchestration-status.json'

// Captured from GET /research/orchestration/status (Research's roster). Refresh it
// when Research adds or renames a schedule; the last test below fails until then.
const ROWS = statusFixture.data.schedules
const INDEX = buildSlotScheduleIndex(ROWS)
const SCHEDULE_NAMES = new Set(ROWS.map(r => r.name))

describe('slotScheduler', () => {
  it('maps every slot Research fires to its schedule', () => {
    expect(dagsterScheduleForSlot('corporate', INDEX)).toBe('market_corporate_schedule')
    expect(dagsterScheduleForSlot('stock-eod', INDEX)).toBe('market_universe_calendar_schedule')
    expect(dagsterScheduleForSlot('fundamentals-rotate', INDEX)).toBe(
      'market_fundamentals_rotate_schedule',
    )
    // The seven slots Console's old hand-kept table never had.
    for (const slot of [
      'fundamentals-market',
      'ratios-market',
      'intraday-chain',
      'treasury',
      'ticker-details',
      'corporate-backfill',
      'option-depth',
    ]) {
      expect(dagsterScheduleForSlot(slot, INDEX), slot).not.toBeNull()
      expect(slotSchedulerKind(slot, INDEX), slot).toBe('dagster')
    }
    // Retired with Options Starter: no schedule fires it.
    expect(dagsterScheduleForSlot('option-trades', INDEX)).toBeNull()
    expect(slotSchedulerLabel('dagster')).toBe('Dagster')
  })

  it('marks analytics slots as Research', () => {
    expect(slotSchedulerKind('max-pain', INDEX)).toBe('research')
    expect(slotSchedulerKind('readiness-refresh', INDEX)).toBe('research')
  })

  it('knows nothing before the roster loads', () => {
    expect(slotSchedulerKind('trim', new Map())).toBe('unknown')
    expect(dagsterScheduleForSlot('trim', new Map())).toBeNull()
  })

  it('names only schedules that exist in the roster', () => {
    // Ratchet (TD-108): a Dagster schedule name written into Console source must
    // be one Research serves, or the page looks up a schedule that is not there.
    const root = path.resolve(__dirname, '../../..')
    const pattern = /['"`]((?:research|market)_[a-z0-9_]+_schedule)['"`]/g
    const unknown: string[] = []
    const walk = (dir: string) => {
      for (const name of readdirSync(dir)) {
        const full = path.join(dir, name)
        if (statSync(full).isDirectory()) {
          if (name !== '__tests__' && name !== 'node_modules') walk(full)
        } else if (/\.(ts|tsx)$/.test(name) && !/\.test\.tsx?$/.test(name)) {
          for (const m of readFileSync(full, 'utf8').matchAll(pattern)) {
            if (!SCHEDULE_NAMES.has(m[1])) unknown.push(`${path.relative(root, full)}: ${m[1]}`)
          }
        }
      }
    }
    walk(root)
    expect(unknown).toEqual([])
  })
})
