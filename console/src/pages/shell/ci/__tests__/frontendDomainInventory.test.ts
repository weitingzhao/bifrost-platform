import fs from 'node:fs'
import path from 'node:path'
import os from 'node:os'
import { fileURLToPath } from 'node:url'
import { expect, it } from 'vitest'
import { buildInventory } from '../../../../../scripts/frontend-domain-map.mjs'

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../../../..')
it('keeps source files and static imports in the published frontend inventory current', () => {
  const declaration = JSON.parse(fs.readFileSync(path.join(repo, 'console/domain-ownership.json'), 'utf8')).frontend
  const actual = buildInventory(repo, declaration)
  expect(actual).toEqual(JSON.parse(fs.readFileSync(path.join(repo, declaration.inventory), 'utf8')))
  const ci = actual.groups.find(g => g.path === 'console/src/pages/shell/ci')!
  expect(ci.imports).toContain('console/src/api/units.ts')
  expect(actual.groups.find(g => g.path === 'console/src/pages/shell/agents')?.imports.length).toBeGreaterThan(0)
  const data = actual.groups.find(g => g.path === 'console/src/pages/shell/data')!
  expect(data.files).toEqual(['console/src/pages/shell/data/DataPage.tsx'])
  expect(declaration.groups[data.path].domains).toEqual(['tenant'])
  const backup = actual.groups.find(g => g.path === 'console/src/domains/foundation/backup')!
  expect(declaration.groups[backup.path].domains).toEqual(['foundation'])
  expect(backup.imports).not.toContain('console/src/api/cluster.ts')
  expect(backup.imports.some(p => p.includes('/pages/') || p.includes('/tenant/'))).toBe(false)
  expect(backup.unresolved_imports).toEqual([])
})

it('resolves aliases, re-exports and literal dynamic imports, keeping unknown imports visible', () => {
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'frontend-domain-'))
 try {
 fs.mkdirSync(path.join(root,'src/child'),{recursive:true})
 fs.writeFileSync(path.join(root,'src/main.ts'),"import type { T } from '@/child/item'; export { v } from './child/item'; import('./child/item'); import(name)")
 fs.writeFileSync(path.join(root,'src/main.test.ts'),"import { v } from './child/item'; import data from '../contract.json'")
 fs.writeFileSync(path.join(root,'src/child/item.ts'),'export const v=1; export type T=number')
 fs.writeFileSync(path.join(root,'contract.json'),'{}')
 const actual=buildInventory(root,{root:'src',groups:{src:{},'src/child':{}}})
 const group=actual.groups.find(g=>g.path==='src')!
 expect(group.imports).toEqual(['src/child'])
 expect(group.test_imports).toEqual(['src/child'])
 expect(group.test_inputs).toEqual(['contract.json'])
 expect(group.unresolved_imports).toEqual(['src/main.ts: non-literal import'])
 } finally { fs.rmSync(root,{recursive:true,force:true}) }
})
