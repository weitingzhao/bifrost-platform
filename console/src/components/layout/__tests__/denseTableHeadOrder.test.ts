// 2026-11-23 UI / Console (箭10 asked for the fix): the Market Data doctor's header stacked SEV / CHECK / SLOT… down
// its first column at 1280, because the table wrapped its row in DenseTableHead (a <th>) and its cells in
// DenseTableHeader (a <thead>): a <th> holding a <tr> holding <thead>s. Bus clients and Bus problem cases had the same
// swap. The right order is DenseTableHeader > DenseTableHeadRow > DenseTableHead. The positive control proves the scan
// sees the swap.
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const SRC = join(__dirname, '..', '..', '..')
const files = (dir: string): string[] => readdirSync(dir).flatMap(n => {
  const p = join(dir, n)
  if (n === 'node_modules') return []
  return statSync(p).isDirectory() ? files(p) : n.endsWith('.tsx') ? [p] : []
})
const swapped = (src: string) => /<DenseTableHead(\s[^>]*)?>\s*<DenseTableHeadRow/.test(src)

describe('dense table head order', () => {
  it('no table wraps its header row in a header cell', () => {
    const offenders = files(SRC).filter(f => swapped(readFileSync(f, 'utf8'))).map(f => f.slice(SRC.length + 1))
    expect(offenders).toEqual([])
  })
  it('positive control: the scan sees the doctor table as it was', () => {
    expect(swapped('<DenseTableHead>\n  <DenseTableHeadRow>\n    <DenseTableHeader>Sev</DenseTableHeader>')).toBe(true)
    expect(swapped('<DenseTableHeader>\n  <DenseTableHeadRow>\n    <DenseTableHead>Sev</DenseTableHead>')).toBe(false)
  })
})
