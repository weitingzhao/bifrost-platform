import assert from 'node:assert/strict'
import { test } from 'node:test'
import { FULL_CI_PATH, fullCICheckPath, unitConfigPath, unitRunsPath, unitScopePath } from './unitsTools.js'

// Origin: 2026-11-14. The units routes need a viewer's sign-in, which agents do
// not have; the CI line itself could not call its own check route that day.
// The Owner: CI's users are the lines up and down the ring and CI itself, and
// they use it through these answers. These are the paths the tools ask.
const a = 'a'.repeat(40)
const b = 'b'.repeat(40)

test('an optional argument left out is left out of the query, not sent empty', () => {
  assert.equal(unitScopePath('proj', a), `/api/v1/units/scope?repo=proj&commit=${a}`)
  assert.equal(unitScopePath('proj', a, b), `/api/v1/units/scope?repo=proj&commit=${a}&base=${b}`)
  assert.equal(unitConfigPath('proj'), '/api/v1/units/config?repo=proj')
  assert.equal(unitConfigPath('proj', a), `/api/v1/units/config?repo=proj&commit=${a}`)
  assert.equal(unitRunsPath('proj'), '/api/v1/units/runs?repo=proj')
  assert.equal(unitRunsPath('proj', 5), '/api/v1/units/runs?repo=proj&limit=5')
  assert.equal(FULL_CI_PATH, '/api/v1/units/full-ci')
  assert.equal(fullCICheckPath('proj', a, b), `/api/v1/units/full-ci/check?repo=proj&green=${a}&red=${b}`)
  assert.equal(fullCICheckPath('proj', a, b, 'ci-run-1'), `/api/v1/units/full-ci/check?repo=proj&green=${a}&red=${b}&run=ci-run-1`)
})

test('a value is encoded: it cannot add a parameter of its own', () => {
  assert.equal(unitRunsPath('x&limit=100'), '/api/v1/units/runs?repo=x%26limit%3D100')
})
