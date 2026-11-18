import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { busEventsPath, regressionsPath } from './busTools.js'
import { PLATFORM_STDIO_TOOL_NAMES } from './stdioToolNames.js'

// Origin: 2026-11-16, the bus's step 1 (api/internal/bus/RULES.md §8 item 4):
// agents wait for events through the gate's long poll, never on NATS; a wait
// is held to the gate's 25 seconds and a value cannot add a parameter.
test('wait_for_event asks the gate with its cursor and at most 25 seconds', () => {
  assert.equal(
    busEventsPath({ types: 'git.main.advanced', after: '9001.42', wait_seconds: 10 }),
    '/api/v1/bus/events?types=git.main.advanced&after=9001.42&wait=10&client=mcp',
  )
  assert.equal(
    busEventsPath({ types: 'git.main.advanced', match: 'bifrost-platform@', wait_seconds: 600, client: 'seat:8.2' }),
    '/api/v1/bus/events?types=git.main.advanced&match=bifrost-platform%40&wait=25&client=seat%3A8.2',
  )
  assert.equal(busEventsPath({ types: 'a.b.c&wait=999' }), '/api/v1/bus/events?types=a.b.c%26wait%3D999&wait=25&client=mcp')
})

test('wait_for_event is registered and listed', () => {
  const index = readFileSync(new URL('./index.ts', import.meta.url), 'utf8')
  assert.match(index, /reg\(\s*'wait_for_event'/)
  assert.ok(new Set<string>(PLATFORM_STDIO_TOOL_NAMES).has('wait_for_event'))
})

// 2026-11-17: the known regressions on main; any seat records one, or its fix.
test('the regression tools are on the full server and read the bus route', () => {
  for (const name of ['report_regression', 'mark_regression_fixed', 'list_regressions']) assert.ok(new Set<string>(PLATFORM_STDIO_TOOL_NAMES).has(name), name)
  assert.equal(regressionsPath({}), '/api/v1/bus/regressions?open=1')
  assert.equal(regressionsPath({ repo: 'bifrost-platform', open: false }), '/api/v1/bus/regressions?repo=bifrost-platform')
})
