// Origin: 0.2 map line, 2026-11-20 — the map's pool read for Agents.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { unperceivedPath } from './catalogTools.js'

test('unperceivedPath escapes the repository name', () => {
  assert.equal(unperceivedPath('bifrost-platform'), '/api/v1/catalog/unperceived?repo=bifrost-platform')
  assert.equal(unperceivedPath('a b'), '/api/v1/catalog/unperceived?repo=a%20b')
})
