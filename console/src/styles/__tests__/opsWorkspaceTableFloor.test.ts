// 2026-11-23 箭10 (after the doctor table): the phone floor for dense tables (560px, 720px from six columns; W-31 batch
// 3, 2026-10-22) was unlayered CSS that outranked a table's own min-width, so the doctor needed !important to be
// wider. The floor now reads --table-min, which a table declares; the default floors stay.
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const css = readFileSync(join(__dirname, '..', 'opsWorkspace.css'), 'utf8')

describe('dense table phone floor', () => {
  it('lets a table declare its own floor and keeps the defaults', () => {
    expect(css).toContain('.ops-workspace .dense-scroll-x > table { min-width: var(--table-min, 560px); }')
    expect(css).toContain('.ops-workspace .dense-scroll-x > table:has(th:nth-child(6)) { min-width: var(--table-min, 720px); }')
  })
})
