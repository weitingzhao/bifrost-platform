// 2026-11-23, the chief of staff's brief (2026-11-22): the Owner could see what each line did only on the Console's
// Commit lineage, never on the City. These cases pin that the City counts a line the way that page does (merged =
// landed, PROD = a deploying prod release contained it), that moving and quiet lines read apart at a glance, and
// that a district says plainly when the roster (thread → domain) is not read instead of guessing.
import { describe, expect, it } from 'vitest'
import type { LineageResponse, LineageThread } from '@/api/lineage'
import { consoleContent } from '../console'
import { districtLines, domainsOf, townLines, type RosterSeat } from '../lines'

const now = new Date('2026-11-23T12:00:00Z')
const commit = (sha: string, landed: boolean, envs: [string, boolean][] = []) => ({
  repo: 'bifrost-platform', sha, subject: sha, at: '2026-11-23T10:00:00Z', ref: 'main', landed,
  reached: envs.map(([env, deploys]) => ({ lane: 'platform', env, run: 'r', at: '2026-11-23T11:00:00Z', deploys })),
})
const thread = (session: string, last_at: string, commits: ReturnType<typeof commit>[], title?: string): LineageThread => ({
  session, title, transcripts: [], first_at: last_at, last_at, commit_count: commits.length,
  landed: commits.filter(c => c.landed).length, reached: {}, repos: [{ repo: 'bifrost-platform', commits }],
})
const lineage = (threads: LineageThread[]): LineageResponse => ({ threads } as unknown as LineageResponse)

describe('lines on the City', () => {
  // 2026-11-23: UI / Console counts the same lines on Commit lineage (lineageActivity.ts); one function, one number.
  it('counts by the furthest stage, as Commit lineage does: PROD also counts as STG and merged', () => {
    const blocks = townLines({ data: lineage([thread('s1', '2026-11-23T10:00:00Z', [
      commit('a', true, [['stg', true], ['prod', true]]),
      commit('b', true, [['stg', true]]),
      commit('c', false),
    ], 'map')]) }, {}, now)
    const t = blocks[1]
    expect(t.kind === 'table' && t.rows[0].cells.slice(2)).toEqual([2, 2, 1])
    expect(t.kind === 'table' && t.rows[0].cells[1]).toEqual({ text: '2 h ago', tone: 'good' })
  })

  it('puts the line picked in the Console first, marked', () => {
    const read = { data: lineage([thread('new', '2026-11-23T11:00:00Z', [commit('y', true)]), thread('old', '2026-11-18T00:00:00Z', [commit('x', true)])]) }
    const t = townLines(read, {}, now, 'old')[1]
    expect(t.kind === 'table' && t.rows.map(r => r.key)).toEqual(['old', 'new'])
    expect(t.kind === 'table' && t.rows[0].cells[0]).toMatchObject({ mark: { text: 'picked' } })
    expect(t.kind === 'table' && t.rows[0].ghost).toBe(true)
  })

  // 2026-11-23, UI / Console: from the City's card the Owner lands on the same line in Commit lineage.
  it('links each line to the same line on Commit lineage', () => {
    const t = townLines({ data: lineage([thread('local_a b', '2026-11-23T11:00:00Z', [commit('y', true)])]) }, {}, now)[1]
    expect(t.kind === 'table' && t.rows[0].href).toBe('#work?section=commit-lineage&thread=local_a%20b')
  })

  it('lists the newest line first and counts the moving ones', () => {
    const blocks = townLines({ data: lineage([thread('old', '2026-11-18T00:00:00Z', [commit('x', true)], 'old line'), thread('new', '2026-11-23T11:00:00Z', [commit('y', true)], 'new line')]) }, {}, now)
    const facts = blocks[0]
    expect(facts.kind === 'facts' && facts.rows[0].value).toEqual({ text: 1, tone: 'good' })
    const table = blocks[1]
    expect(table.kind === 'table' && table.rows.map(r => r.key)).toEqual(['new', 'old'])
    expect(blocks.at(-1)).toMatchObject({ kind: 'note', text: expect.stringContaining('seat roster is not served') })
  })

  it('a failed lineage read says not known, never an empty table', () => {
    expect(townLines({ error: new Error('lineage: HTTP 502') }, {}, now)).toEqual([{ kind: 'note', text: 'What the lines did is not known: lineage: HTTP 502' }])
  })

  // Crew, 2026-11-23: one session may sit two seats (2-lineage and 12-audit): count it in both domains.
  it('hangs a line on every domain its session sits, once the roster is read', () => {
    const roster: RosterSeat[] = [
      { id: '2-lineage', kind: 'owner', session: 's2', domain_id: 'lineage' },
      { id: '12-audit', kind: 'owner', session: 's2', domain_id: 'audit' },
      { id: '3-4', kind: 'owner', session: 's3', domain_id: 'verification', also_domain_ids: ['delivery'] },
    ]
    expect(domainsOf(roster, 's2')).toEqual(['lineage', 'audit'])
    expect(domainsOf(roster, 's3')).toEqual(['verification', 'delivery'])
    const read = { data: lineage([thread('s3', '2026-11-23T11:00:00Z', [commit('z', true)])]) }
    const delivery = districtLines(read, { data: roster }, 'delivery', now)
    expect(delivery[0].kind === 'table' && delivery[0].rows.map(r => r.key)).toEqual(['s3'])
    expect(districtLines(read, { data: roster }, 'audit', now)).toContainEqual({ kind: 'note', text: expect.stringContaining('1 seat of this domain committed nothing') })
  })

  it('a district without the roster says so and points to the city level', () => {
    expect(districtLines({ data: lineage([]) }, { error: 'no read yet' }, 'plan', now)[0]).toMatchObject({ kind: 'note', text: expect.stringContaining('The lines are on the city level') })
  })

  it('the city level carries the lines card first; a district its own', () => {
    const domains = { data: { version: 1, at: '', commit: '', domains: [{ id: 'plan', overall: 'good' }] } } as never
    const c = consoleContent(domains, null, {}, undefined, undefined, undefined, {}, { read: { data: lineage([]) }, roster: {}, now })
    expect(c.panels?.(c.tree)[0].id).toBe('lines')
    const plan = c.tree.children.find(p => p.id === 'plan')!
    expect(c.panels?.(plan).map(p => p.id)).toContain('lines')
  })
})
