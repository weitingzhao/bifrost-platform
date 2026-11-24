// 2026-11-23 UI / Console (the design-system inventory): state colours come from the engine (STATUS in
// @bifrost/ui/city/world), not Tailwind's raw palette. The last raw uses (cluster node gauges, the node wizard,
// arch icons, Bus "not measured") moved to the engine this day; this scan keeps the count at zero. The positive
// control proves it sees a raw class.
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const SRC = join(__dirname, '..', '..', '..')
const files = (dir: string): string[] => readdirSync(dir).flatMap(n => {
  const p = join(dir, n)
  if (n === '__tests__' || n === 'node_modules') return []
  return statSync(p).isDirectory() ? files(p) : n.endsWith('.tsx') ? [p] : []
})
const RAW = /\b(?:text|bg|border|fill|stroke|ring)-(?:emerald|amber|red|green|yellow|rose|orange|blue|sky|zinc)-\d{3}\b/

describe('no raw state palette', () => {
  it('no component paints with the raw palette', () => {
    const offenders = files(SRC).filter(f => RAW.test(readFileSync(f, 'utf8'))).map(f => f.slice(SRC.length + 1))
    expect(offenders).toEqual([])
  })
  it('positive control: the scan sees the node gauge as it was', () => {
    expect(RAW.test("return 'bg-emerald-500'")).toBe(true)
    expect(RAW.test('<span className="text-sm text-amber-600">Not measured')).toBe(true)
    expect(RAW.test("style={{ color: STATUS.unknown.ink }}")).toBe(false)
  })
})
