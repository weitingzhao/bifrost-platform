// 2026-11-21, the Owner: "set the boundary from the start" — nothing but the engine draws the UI. The content is a
// translation layer: it reads the system and hands the engine data (places, links, the tech tree, cards as data),
// never markup. This holds the wall: no .tsx file, no React, no JSX under content/. The positive control below
// proves the scan sees markup when it is there.
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const ROOT = join(__dirname, '..')
const files = (dir: string): string[] => readdirSync(dir).flatMap(n => {
  const p = join(dir, n)
  if (n === '__tests__') return []
  return statSync(p).isDirectory() ? files(p) : /\.(ts|tsx)$/.test(n) ? [p] : []
})
const drawsUI = (src: string, name: string) => name.endsWith('.tsx') || /from ['"]react['"]/.test(src) || /from ['"]react-dom/.test(src) || /<\/[a-z]+>|\/>\s*[,)\]]/.test(src)

describe('the content draws nothing', () => {
  it('no file under content/ holds markup or React', () => {
    const offenders = files(ROOT).filter(f => drawsUI(readFileSync(f, 'utf8'), f)).map(f => f.slice(ROOT.length + 1))
    expect(offenders).toEqual([])
  })
  it('positive control: the scan sees a table drawn by hand (the case of domainsPanel.tsx before 2026-11-21)', () => {
    expect(drawsUI("import { useState } from 'react'\nconst T = () => <table className=\"g2-table\"></table>", 'x.ts')).toBe(true)
    expect(drawsUI('export const n = 1', 'x.tsx')).toBe(true)
    expect(drawsUI("const t: Block = { kind: 'note', text: 'a < b' }", 'x.ts')).toBe(false)
  })
})
