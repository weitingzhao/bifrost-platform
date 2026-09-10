import { describe, expect, it } from 'vitest'
import {
  buildMatrix,
  changeIndex,
  changeSummary,
  entryOf,
} from '@/components/market-data/coverageMatrixModel'
import { breadthVerdict, depthVerdict } from '@/components/market-data/dimensionsModel'
import { buildCoverageMatrixPack } from '@/components/market-data/coverageMatrixPack'
import type { CoverageMemory, DatasetDimensions } from '@/api/marketDataDimensions'

const ds = (over: Partial<DatasetDimensions> = {}) =>
  ({
    dataset: 'raw_market.option_daily',
    tier: 'universe',
    grain: 'daily',
    slots: ['option-bars'],
    breadth_window: 'session',
    error: null,
    breadth: { held: 25, held_total: 25, outside_scope: 0, of: 575, pct: 4.3, entitlement_pct: null },
    depth: { target: { kind: 'rolling_days', value: 730, why: '' }, measured: true, at_target: 503, of: 575 },
    freshness: { newest: '2026-09-10', deadline_hours: 30, measured: true, days_behind: 0 },
    continuity: { measured: true, days_present: 60, days_absent: 0, days_thin: 0 },
    ...over,
  }) as unknown as DatasetDimensions

const memory = (over: Partial<CoverageMemory> = {}): CoverageMemory => ({
  recorded: true,
  changed_at: '2026-09-11T22:47:00+00:00',
  previous_at: '2026-09-04T22:47:00+00:00',
  samples: 2,
  changes: [],
  ...over,
})

describe('the verdict the plugin declared wins', () => {
  it('is used instead of re-deriving one here', () => {
    // C-G1: the contract table is the only source of thresholds. The console
    // keeps its rules only as the fallback for an older plugin.
    const d = ds({ verdicts: { breadth: 'ok', depth: 'thin' } } as Partial<DatasetDimensions>)
    expect(breadthVerdict(d)).toBe('ok') // 4.3% would otherwise be thin
    expect(depthVerdict(d)).toBe('thin') // 503/575 would otherwise be partial
  })

  it('falls back to the local rules when the payload carries none', () => {
    expect(breadthVerdict(ds())).toBe('thin')
    expect(depthVerdict(ds())).toBe('partial')
  })
})

describe('a mark remembers how it got here', () => {
  it('carries the move onto the axis it happened to, and only that one', () => {
    const idx = changeIndex(
      memory({
        changes: [
          {
            dataset: 'raw_market.option_daily',
            axis: 'breadth',
            from: 'ok',
            to: 'thin',
            direction: 'regressed',
          },
        ],
      }),
    )
    const axes = entryOf(ds(), idx).axes
    const byAxis = Object.fromEntries(axes.map(a => [a.axis, a.change]))
    expect(byAxis.breadth?.direction).toBe('regressed')
    expect(byAxis.depth).toBeNull()
    expect(byAxis.freshness).toBeNull()
  })

  it('leaves every mark unmarked when nothing moved', () => {
    const cells = buildMatrix([ds()], changeIndex(memory()))
    expect(cells.flat().flatMap(c => c.entries).flatMap(e => e.axes).every(a => a.change === null)).toBe(true)
  })
})

describe('three states a single count would flatten', () => {
  it('tells a first reading apart from a quiet one', () => {
    // "Nothing changed" and "nothing to compare against" are different claims.
    expect(changeSummary(memory({ previous_at: null })).firstReading).toBe(true)
    expect(changeSummary(memory()).firstReading).toBe(false)
  })

  it('counts by direction rather than reporting a total', () => {
    const s = changeSummary(
      memory({
        changes: [
          { dataset: 'a', axis: 'breadth', from: 'ok', to: 'thin', direction: 'regressed' },
          { dataset: 'b', axis: 'depth', from: 'thin', to: 'ok', direction: 'recovered' },
          { dataset: 'c', axis: 'depth', from: 'thin', to: 'boundary', direction: 'changed' },
        ],
      }),
    )
    expect(s).toMatchObject({ regressed: 1, recovered: 1, other: 1, total: 3 })
  })

  it('reports a record that could not be written', () => {
    const s = changeSummary({ recorded: false, why: 'no such table', changes: [] })
    expect(s.total).toBe(0)
    expect(s.firstReading).toBe(false)
  })
})

describe('the agent brief says what moved', () => {
  it('leads with the regression rather than the standing gap', () => {
    const text = buildCoverageMatrixPack(
      {
        datasets: [ds()],
        memory: memory({
          changes: [
            {
              dataset: 'raw_market.option_daily',
              axis: 'breadth',
              from: 'ok',
              to: 'thin',
              direction: 'regressed',
            },
          ],
        }),
      },
      '2026-09-12T00:00:00Z',
    )
    expect(text).toContain('## Since last reading — 1 verdict(s) moved')
    expect(text).toContain('WORSE · raw_market.option_daily breadth: ok → thin')
    expect(text).toContain('a regression has a cause')
  })

  it('says so when there is nothing to compare against', () => {
    const text = buildCoverageMatrixPack(
      { datasets: [ds()], memory: memory({ previous_at: null }) },
      '2026-09-12T00:00:00Z',
    )
    expect(text).toContain('first recorded reading')
    expect(text).not.toContain('No verdict changed')
  })

  it('never claims calm when the record failed to write', () => {
    const text = buildCoverageMatrixPack(
      { datasets: [ds()], memory: { recorded: false, why: 'no such table', changes: [] } },
      '2026-09-12T00:00:00Z',
    )
    expect(text).toContain('Not known')
    expect(text).toContain('no such table')
  })
})
