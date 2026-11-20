// Origin: 0.2 map line, 2026-11-20 — the map's pool read for Agents.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { ownerPath, unperceivedPath } from './catalogTools.js'

test('ownerPath escapes both parameters', () => {
  assert.equal(ownerPath('bifrost-platform', 'api/internal/x y'), '/api/v1/catalog/owner?repo=bifrost-platform&path=api%2Finternal%2Fx%20y')
})

test('unperceivedPath escapes the repository name', () => {
  assert.equal(unperceivedPath('bifrost-platform'), '/api/v1/catalog/unperceived?repo=bifrost-platform')
  assert.equal(unperceivedPath('a b'), '/api/v1/catalog/unperceived?repo=a%20b')
})
