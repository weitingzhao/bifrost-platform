import assert from 'node:assert/strict'
import { test } from 'node:test'
import { bifrostSessionId, sessionFellBack } from './sessionId.js'

const host = () => 'mac-mini'

// Origin: 2026-11-21, Station asked whether a Codex seat can use the doorbell.
// It could not: the session id read only Claude's and Cursor's keys, so a Codex
// thread fell back to the hostname and the doorbell refused (no session id).
// A Codex thread is now itself, and one a Claude session dispatched (which
// inherits CLAUDE_CODE_HOST_SESSION_ID) is still the Codex thread, not its
// parent (TD-577).
test('a Codex thread names itself, before an inherited Claude session', () => {
  assert.equal(bifrostSessionId({ CODEX_THREAD_ID: 'codex-1' }, host), 'codex-1')
  assert.equal(sessionFellBack({ CODEX_THREAD_ID: 'codex-1' }), false)
  assert.equal(bifrostSessionId({ CODEX_THREAD_ID: 'codex-1', CLAUDE_CODE_HOST_SESSION_ID: 'local_parent' }, host), 'codex-1')
})

// Positive control for the test above: Claude, Cursor and the fallback are as they were.
test('Claude, then Cursor, then the hostname, as before', () => {
  assert.equal(bifrostSessionId({ CLAUDE_CODE_HOST_SESSION_ID: 'local_a', CURSOR_CONVERSATION_ID: 'c' }, host), 'local_a')
  assert.equal(bifrostSessionId({ CURSOR_CONVERSATION_ID: 'c' }, host), 'c')
  assert.equal(bifrostSessionId({ CODEX_THREAD_ID: '  ' }, host), 'mac-mini')
  assert.equal(sessionFellBack({}), true)
})
