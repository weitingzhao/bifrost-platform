// 2026-11-24 (ISSUE-17): an entry on the Console is enough — step one is the
// framework of laws itself on Command → Laws, empty for now: five levels, the fields, the rules between laws and
// what refuses a card, and the count in force. 2026-11-25, LAW-1 四 / ISSUE-104: the register holds laws only; the
// switch for the other rows and the coverage line are gone and the page draws only laws in force.
// What the platform does not send is "unknown", never filled in. Fixture: planstore TheFramework (d7ab0b4b8), cut.
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { LawFramework, RegisterLaw } from '@/api/lawRegister'

const wire = vi.hoisted(() => ({ read: vi.fn(), env: 'prod' }))
vi.mock('@/lib/platformAuth', () => ({ getPlatformOperatorToken: () => 'test-viewer' }))
vi.mock('@/api/serverContact', () => ({ serverFetch: (...a: unknown[]) => wire.read(...a) }))
vi.mock('@/api/core', () => ({ fetchSelfHealth: async () => ({ viewer_env: wire.env }) }))

import { engineLawsOn } from '@/api/lawRegister'
import { LawRegister } from '@/pages/shell/owner/LawRegister'
import { LawFrameworkView } from '@/pages/shell/owner/LawFramework'
import { frameworkLine, inForceAt, inForceCount, inForceLaws, inForceWithoutLevel, purgeLine, requiredLabels, stateCounts, stateTally } from '@/pages/shell/owner/lawFrameworkModel'

const framework: LawFramework = {
  version: 'v1', source: '法的框架 v1, approved by the Owner',
  levels: [
    { level: 0, name: '道', meaning: '信念和方向', required: ['text', 'source'], in_force: 0 },
    { level: 1, name: '根本法', meaning: '收不回的硬边界（六类）', required: ['text', 'source', 'scope'], in_force: 0 },
    { level: 2, name: '基本法', meaning: '全系统通用的规矩', required: ['text'], in_force: 0 },
    { level: 3, name: '领域法', meaning: '只适用于某个领域', required: ['text', 'parent'], in_force: 0 },
    { level: 4, name: '细则与判例', meaning: '落实上位法的具体做法', required: ['text', 'parent'], in_force: 0 },
  ],
  fields: [{ name: 'text', label: '原文', meaning: '一句话说清规矩' }, { name: 'source', label: '出处', meaning: '谁在哪天说的' }, { name: 'scope', label: '适用范围', meaning: '全系统…' }],
  subjects: ['交易和钱'], scope_forms: ['全系统'], modalities: ['必须', '禁止', '允许'], enforce: ['未执行', '执行中'],
  states: { 现行: 2 },
  between: ['上位优先：下位法和上位法抵触的那一处不执行'],
  integrity: ['法只经 Owner 批准的修法票增、改、废'],
  stage_note: '现阶段框架的发现只在卡面提示、不拦',
}
const law = (n: string, state?: string, level?: number): RegisterLaw => ({
  law: n, version: 1, op: 'transcribed', text: `law ${n}`, status: '已落', source: 'OWNER_LAW.md', at: '2026-11-24T00:00:00Z',
  ...(state ? { state } : {}), ...(level !== undefined ? { frame: { level } } : {}),
})
const json = (b: unknown) => new Response(JSON.stringify(b), { status: 200, headers: { 'content-type': 'application/json' } })

afterEach(() => { cleanup(); wire.read.mockReset(); wire.env = 'prod'; window.location.hash = '' })

describe('the framework model', () => {
  it('says the frame in one line, and a framework not sent is unknown', () => {
    expect(frameworkLine(framework)).toBe('framework v1 · 5 levels · 现行 2')
    expect(frameworkLine(null)).toBe('framework not sent by this platform: unknown')
    // A state with no count is "?", never 0.
    expect(stateCounts({ states: {} })).toEqual([['现行', null]])
    // 2026-11-24 立法线: the repealed leave the register; with no 废止 key the page shows no 废止, not "废止 0" or unknown.
    expect(frameworkLine({ ...framework, states: { 现行: 22 } })).toBe('framework v1 · 5 levels · 现行 22')
    // 2026-11-25, LAW-1 四: a state an older platform still sends is shown after the known ones, never dropped silently.
    expect(stateCounts({ states: { 现行: 22, X: 0 } })).toEqual([['现行', 22], ['X', 0]])
  })
  it('names the required fields by their labels', () => {
    expect(requiredLabels(framework.levels![1], framework.fields)).toEqual(['原文', '出处', '适用范围'])
    expect(requiredLabels(framework.levels![3], framework.fields)).toEqual(['原文', 'parent'])
  })
  it('counts the register by state: 现行, and 废止 only while such rows come', () => {
    const laws = [law('2', '现行', 2), law('3', '废止'), law('4')]
    expect(stateTally(laws)).toBe('现行 1 · 废止 1 · state unknown 1')
    expect(stateTally([law('2', '现行', 2)])).toBe('现行 1')
    expect(inForceCount(laws)).toBe(1)
  })
  // 2026-11-25, LAW-1 四 / ISSUE-104: a law in force without a level is a law with a field missing, listed as such, not
  // a state of its own; the page's rows are the laws in force only (the repealed and stateless rows are the control).
  it('hangs laws in force under their level, lists those without one, and draws only laws in force', () => {
    const laws = [law('9', '现行'), law('2', '现行', 2), law('3', '废止'), law('4')]
    expect(inForceAt(laws, 2).map(l => l.law)).toEqual(['2'])
    expect(inForceWithoutLevel(laws).map(l => l.law)).toEqual(['9'])
    expect(inForceLaws(laws).map(l => l.law)).toEqual(['2', '9'])
  })
})

describe('the framework on the page', () => {
  it('shows the five blocks with the framework words, in force 0 on every level', () => {
    render(<LawFrameworkView framework={{ ...framework, states: { 现行: 0 } }} laws={[law('3', '废止')]} />)
    expect(document.querySelectorAll('[data-level]')).toHaveLength(5)
    expect(screen.getByText('根本法')).toBeTruthy()
    expect([...document.querySelectorAll('[data-in-force]')].map(e => e.textContent)).toEqual(Array(5).fill('In force 0'))
    expect(document.querySelector('[data-field="scope"]')?.textContent).toContain('适用范围')
    expect(screen.getByText('上位优先：下位法和上位法抵触的那一处不执行')).toBeTruthy()
    expect(screen.getByText('法只经 Owner 批准的修法票增、改、废')).toBeTruthy()
    expect(document.querySelector('[data-state="现行"]')?.textContent).toBe('现行 0')
    // 2026-11-25, LAW-1 四 / ISSUE-104: no coverage line, no level-missing list when every law has a level.
    expect(document.querySelector('[data-coverage]')).toBeNull()
    expect(document.querySelector('[data-no-level]')).toBeNull()
  })
  it('hangs a law in force under its level by its code, and lists one without a level apart', () => {
    const inForce: RegisterLaw = { ...law('90', '现行', 2), code: 'LAW-5' }
    render(<LawFrameworkView framework={framework} laws={[inForce, law('7', '现行')]} />)
    expect(document.querySelector('[data-level="2"] [data-in-force-law="90"]')?.textContent).toBe('LAW-5: law 90')
    expect(document.querySelector('[data-no-level]')?.textContent).toContain('In force, level missing 1')
    expect(document.querySelector('[data-no-level] [data-in-force-law="7"]')?.textContent).toBe('#7: law 7')
  })
  it('says unknown where the platform sent nothing, never an empty or made-up value', () => {
    render(<LawFrameworkView framework={{ version: 'v1', levels: [{ level: 0, name: '道', meaning: '', required: ['text'], in_force: 0 }] }} laws={[]} />)
    expect(document.querySelector('[data-framework-block="between"]')?.textContent).toContain('unknown')
    expect(document.querySelector('[data-framework-block="fields"]')?.textContent).toContain('unknown')
    expect(document.querySelector('[data-state="现行"]')?.textContent).toBe('现行 unknown')
  })
})

describe('Command → Laws reads', () => {
  const body = { framework, laws: [{ ...law('90', '现行', 2), code: 'LAW-5' }, { ...law('91', '现行', 2), code: 'LAW-6' }, law('11', '废止')] }
  const mount = () => render(<QueryClientProvider client={new QueryClient()}><LawRegister /></QueryClientProvider>)

  it('through the engine on STG only', () => {
    expect(engineLawsOn('stg')).toBe(true)
    expect(engineLawsOn('prod')).toBe(false)
    expect(engineLawsOn(undefined)).toBe(false)
  })
  it('on STG reads /engine/v1/agents?read=laws and draws the frame and the laws in force only', async () => {
    wire.env = 'stg'
    wire.read.mockResolvedValue(json({ version: 1, model: 'agents:laws', capturedAt: '2026-11-24T08:00:00Z', provenance: 'live', identity: { epoch: 'e1', role: 'viewer' }, evidence: [{ source: '/api/v1/agentrt/laws', outcome: 'read', receivedAt: '2026-11-24T08:00:00Z' }], data: body }))
    mount()
    await waitFor(() => expect(document.querySelectorAll('[data-level]')).toHaveLength(5))
    expect(wire.read.mock.calls.map(c => c[0])).toEqual(['/engine/v1/agents?read=laws'])
    expect(screen.getByText('framework v1 · 5 levels · 现行 2')).toBeTruthy()
    // 2026-11-24 立法线: "85 laws" was read as 85 laws in the framework. The head counts by state; no "N laws".
    expect(document.body.textContent).not.toMatch(/\d+ laws/)
    // 2026-11-25, LAW-1 四 / ISSUE-104: no switch; the laws in force are drawn, the repealed one is not.
    expect(document.querySelector('[data-laws-in-force]')?.textContent).toContain('LAW-5')
    expect(document.body.textContent).toContain('LAW-6')
    expect(screen.queryByText('law 11')).toBeNull()
    expect(screen.queryByRole('checkbox')).toBeNull()
  })
  it('says engine unreachable on STG and does not read the platform instead', async () => {
    wire.env = 'stg'
    wire.read.mockRejectedValue(new TypeError('Failed to fetch'))
    mount()
    await waitFor(() => expect(screen.getByText(/engine unreachable: Failed to fetch/)).toBeTruthy())
    expect(wire.read.mock.calls.map(c => c[0])).toEqual(['/engine/v1/agents?read=laws'])
  })
  it('elsewhere reads the platform directly; an older platform without the framework says unknown', async () => {
    wire.read.mockResolvedValue(json({ laws: [law('1')] }))
    mount()
    await waitFor(() => expect(screen.getByText('This platform does not send the framework yet: unknown.')).toBeTruthy())
    expect(String(wire.read.mock.calls[0][0])).toBe('/api/v1/agentrt/laws')
  })
})

// Origin: 2026-11-25, LAW-1 s5 had no reading on the page: the repealed are not shown, only how many wait to be
// deleted and whether any is overdue; a platform that does not send it says unknown, never 0.
describe('purge queue line', () => {
  it('says the numbers, nothing waiting, and unknown', () => {
    expect(purgeLine({ waiting: 16, overdue: 0, next_purge_at: '2026-12-01T20:04:00Z' })).toBe('repealed awaiting deletion 16 · overdue 0 · next 2026-12-01')
    expect(purgeLine({ waiting: 0, overdue: 0 })).toBe('repealed awaiting deletion 0')
    expect(purgeLine(null)).toBe('repealed awaiting deletion: unknown')
  })
})
