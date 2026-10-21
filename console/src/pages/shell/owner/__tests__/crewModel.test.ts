import { describe, expect, it } from 'vitest'
import type { RuntimeAttempt, RuntimeHost } from '@/api/agentRuntime'
import { crewRows, duration, firstLine } from '../crewModel'

describe('duration', () => {
  it.each([
    [undefined, 'not started'],
    [0, '0 s'],
    [59, '59 s'],
    [60, '1 min'],
    [3599, '59 min'],
    [3600, '1 h 0 min'],
    [86399, '23 h 59 min'],
    [86400, '24 h 0 min'],
    [90061, '25 h 1 min'],
  ])('formats %s seconds as %s', (seconds, expected) => {
    expect(duration(seconds)).toBe(expected)
  })

  it('treats null as not started at runtime', () => {
    // Null is handled at runtime even though the public type only allows undefined.
    // @ts-expect-error Exercise the null boundary without changing the model's type.
    expect(duration(null)).toBe('not started')
  })
})

describe('firstLine', () => {
  it.each(['', '\n', '\nsecond line', '   \nsecond line'])('keeps a blank first line for %j', title => {
    expect(firstLine(title)).toBe('')
  })

  it('uses only the first line', () => {
    expect(firstLine('First line\nSecond line')).toBe('First line')
  })

  it('trims surrounding spaces and CRLF before measuring', () => {
    const title = 'x'.repeat(80)
    expect(firstLine(`  ${title}  \r\nSecond line`)).toBe(title)
  })

  it('leaves exactly 80 characters unchanged', () => {
    expect(firstLine('x'.repeat(80))).toBe('x'.repeat(80))
  })

  it.each([81, 1000])('cuts %i characters to 80 including the ellipsis', length => {
    expect(firstLine('x'.repeat(length))).toBe(`${'x'.repeat(79)}…`)
  })

  it('cuts a long Chinese title to 80 including the ellipsis', () => {
    const title = '这是一个很长的中文标题'.repeat(10)
    expect(firstLine(title)).toBe(`${title.slice(0, 79)}…`)
    expect(firstLine(title)).toHaveLength(80)
  })

  it('honours a custom maximum including the ellipsis', () => {
    expect(firstLine('abcde', 5)).toBe('abcde')
    expect(firstLine('abcdef', 5)).toBe('abcd…')
  })

  it('omits an emoji that straddles the cut without leaving a lone surrogate', () => {
    const result = firstLine(`${'x'.repeat(78)}😀more text`)
    expect(result).toBe(`${'x'.repeat(78)}…`)
    expect(result.length).toBeLessThanOrEqual(80)
    expect(decodeURIComponent(encodeURIComponent(result))).toBe(result)
  })

  it('keeps an emoji that fits entirely before the cut', () => {
    const result = firstLine(`${'x'.repeat(77)}😀more text`)
    expect(result).toBe(`${'x'.repeat(77)}😀…`)
    expect(result).toHaveLength(80)
    expect(decodeURIComponent(encodeURIComponent(result))).toBe(result)
  })

  it('honours a custom maximum in UTF-16 units with an emoji', () => {
    expect(firstLine('xx😀more', 4)).toBe('xx…')
    expect(firstLine('xx😀more', 5)).toBe('xx😀…')
    expect(firstLine('xx😀', 4)).toBe('xx😀')
  })
})

const host = (id: string, state = 'active'): RuntimeHost => ({ host_id: id, state, last_poll_at: '' })
const attempt = (id: string, hostId: string): RuntimeAttempt => ({
  attempt_id: id, host_id: hostId, task_id: 'task', mission_id: 'mission',
  executor: 'exec:codex', model: '', state: 'running', status: 'running', reason: '',
})

describe('crewRows', () => {
  it('returns no rows without hosts or attempts', () => {
    expect(crewRows([], [])).toEqual([])
  })

  it('puts an attempt with an empty host id in the no host row', () => {
    const live = attempt('a', '')
    expect(crewRows([], [live])).toEqual([{ id: 'no host', host: undefined, attempts: [live], lost: [] }])
  })

  it('gives an unregistered host a row without host metadata', () => {
    const live = attempt('a', 'unknown')
    expect(crewRows([], [live])).toEqual([{ id: 'unknown', host: undefined, attempts: [live], lost: [] }])
  })

  it('omits idle retired hosts but gives their attempts a row without host metadata', () => {
    const retired = host('old', 'retired')
    const live = attempt('a', 'old')
    expect(crewRows([retired], [])).toEqual([])
    expect(crewRows([retired], [live])).toEqual([{ id: 'old', host: undefined, attempts: [live], lost: [] }])
  })

  it('sorts busy rows first and then sorts each group by id', () => {
    const hosts = [host('idle-z'), host('busy-z'), host('idle-a'), host('busy-a')]
    const live = [attempt('a', 'busy-z'), attempt('b', 'busy-a')]
    expect(crewRows(hosts, live).map(row => row.id)).toEqual(['busy-a', 'busy-z', 'idle-a', 'idle-z'])
  })

  it('keeps two attempts on the same host in one row', () => {
    const registered = host('shared')
    const live = [attempt('a', 'shared'), attempt('b', 'shared')]
    expect(crewRows([registered], live)).toEqual([{ id: 'shared', host: registered, attempts: live, lost: [] }])
  })
  it('keeps a lost attempt whose result is not persisted under its host, before idle hosts (TD-352)', () => {
    const lost = { ...attempt('l', 'gone'), state: 'lost', persisted: false }
    const kept = { ...attempt('k', 'gone'), state: 'lost', persisted: true }
    const old = { ...attempt('o', 'gone'), state: 'lost' } // a platform before TD-352 says nothing of it
    const rows = crewRows([host('idle'), host('gone')], [], [lost, kept, old])
    expect(rows.map(r => r.id)).toEqual(['gone', 'idle'])
    expect(rows[0]).toMatchObject({ attempts: [], lost: [lost] })
  })
})
