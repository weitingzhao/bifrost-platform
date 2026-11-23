// 2026-11-23, «one world» baseline (cockpit/ui-world/frame/proposal.md, item 6): the tech tree had glyphs for 9
// of the 14 domains (crew, station, audit, triage, assembly showed initials) and no other view had any. These
// cases pin that every domain the ownership table declares has a glyph, and that each is one the engine draws.
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { GLYPHS } from '@bifrost/ui/city/worldGlyphs'
import { DOMAIN_GLYPH } from '../domainGlyphs'

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../../../../../..')
const declared = (JSON.parse(fs.readFileSync(path.join(repo, 'config/domain-ownership.json'), 'utf8')) as { domains: { id: string }[] }).domains.map(d => d.id)

describe('domain glyphs, one table for every view', () => {
  it('names a glyph for every domain the ownership table declares', () => {
    expect(declared.length).toBeGreaterThan(0)
    expect(declared.filter(id => !DOMAIN_GLYPH[id])).toEqual([])
  })

  it('names only glyphs the engine draws', () => {
    expect(Object.values(DOMAIN_GLYPH).filter(g => !GLYPHS[g])).toEqual([])
  })
})
