import { afterEach, expect, it } from 'vitest'
import { clearContextAdapters, placeHrefs, registerContextAdapter } from '@/lib/shell/sharedContext'
import { consoleContext } from '@/lib/shell/consoleContext'
import { signalsContext } from '../signalsContext'

// 2026-11-25, 箭10 (gap ③ of the progress score): switching place keeps what was looked at. Signals holds the opened
// lamp group in its address; a device is the focus node, a domain the scope; what Signals cannot show travels as from=.
afterEach(() => clearContextAdapters())

it('reads its address: an opened device is the focus node, an opened domain the scope, anything else nothing', () => {
  expect(signalsContext.toContext('#observation?open=device%3Amini-pc-a')).toEqual({ focus: { kind: 'node', id: 'mini-pc-a' } })
  expect(signalsContext.toContext('#observation?open=domain%3Adelivery')).toEqual({ scope: 'delivery' })
  expect(signalsContext.toContext('#observation?open=ci%3Abifrost-ci-infra')).toEqual({})
  expect(signalsContext.toContext('#observation')).toEqual({})
})

it('opens what the context names: a node its device, a scope its domain; a lane it cannot show travels as from=', () => {
  expect(signalsContext.hrefFor({ focus: { kind: 'node', id: 'gpu-server' } })).toEqual({ href: '#observation?open=device%3Agpu-server', shown: true })
  expect(signalsContext.hrefFor({ scope: 'delivery' })).toEqual({ href: '#observation?open=domain%3Adelivery', shown: true })
  expect(signalsContext.hrefFor({})).toBeNull()
  registerContextAdapter('console', consoleContext); registerContextAdapter('signals', signalsContext)
  const fromConsole = placeHrefs({ signals: '#observation' }, { scope: 'delivery', focus: { kind: 'lane', id: 'I4cec' } }, 'console')
  expect(fromConsole.signals).toBe('#observation?open=domain%3Adelivery&from=lane%3AI4cec')
  const fromSignals = placeHrefs({ console: '#system' }, { focus: { kind: 'node', id: 'gpu-server' } }, 'signals')
  expect(fromSignals.console).toBe('#system') // the Console shows no node: its link stays as the host gives it
})
