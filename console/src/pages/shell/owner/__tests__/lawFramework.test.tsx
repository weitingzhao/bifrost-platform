// 2026-11-24 the Owner (by the legislation line): "我要在 console 的那个里面有一个入口能看到就可以了" — step one is the
// framework of laws itself on Command → Laws, empty for now: five levels, the fields, the rules between laws and
// what refuses a card, the states' counts, and the material and repealed laws behind a switch, off by default.
// What the platform does not send is "unknown", never filled in. Fixture: planstore TheFramework (d7ab0b4b8), cut.
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { LawFramework, RegisterLaw } from '@/api/lawRegister'

const wire = vi.hoisted(() => ({ read: vi.fn(), env: 'prod' }))
vi.mock('@/lib/platformAuth', () => ({ getPlatformOperatorToken: () => 'test-viewer' }))
vi.mock('@/api/serverContact', () => ({ serverFetch: (...a: unknown[]) => wire.read(...a) }))
vi.mock('@/api/core', () => ({ fetchSelfHealth: async () => ({ viewer_env: wire.env }) }))

import { engineLawsOn } from '@/api/lawRegister'
import { LawRegister } from '@/pages/shell/owner/LawRegister'
import { LawFrameworkView } from '@/pages/shell/owner/LawFramework'
import { behindSwitch, coverageLine, frameworkLine, inForceAt, inForceCount, materialOrder, requiredLabels, stateCounts, stateTally } from '@/pages/shell/owner/lawFrameworkModel'
import { lawRegisterTable } from '@/pages/shell/owner/lawRegisterModel'

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
  states: { 现行: 0, 素材: 2, 废止: 0 },
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
    expect(frameworkLine(framework)).toBe('framework v1 · 5 levels · 现行 0 · 素材 2 · 废止 0')
    expect(frameworkLine(null)).toBe('framework not sent by this platform: unknown')
    // A state with no count is "?", never 0.
    expect(stateCounts({ states: { 现行: 0 } })).toEqual([['现行', 0], ['素材', null]])
    // 2026-11-24 立法线: the repealed leave the register; with no 废止 key the page shows no 废止, not "废止 0" or unknown.
    expect(frameworkLine({ ...framework, states: { 现行: 0, 素材: 69 } })).toBe('framework v1 · 5 levels · 现行 0 · 素材 69')
  })
  it('names the required fields by their labels', () => {
    expect(requiredLabels(framework.levels![1], framework.fields)).toEqual(['原文', '出处', '适用范围'])
    expect(requiredLabels(framework.levels![3], framework.fields)).toEqual(['原文', 'parent'])
  })
  it('counts the register by state: only 现行 is a law', () => {
    const laws = [law('1', '素材'), law('2', '现行', 2), law('3', '废止'), law('4')]
    expect(stateTally(laws)).toBe('现行 1 · 素材 1 · 废止 1 · state unknown 1')
    expect(stateTally([law('1', '素材')])).toBe('现行 0 · 素材 1')
    expect(inForceCount(laws)).toBe(1)
  })
  it('hangs only laws in force under their level; everything else is behind the switch', () => {
    const laws = [law('1', '素材'), law('2', '现行', 2), law('3', '废止'), law('4')]
    expect(inForceAt(laws, 2).map(l => l.law)).toEqual(['2'])
    expect(behindSwitch(laws).map(l => l.law)).toEqual(['1', '3', '4'])
  })
})

describe('the framework on the page', () => {
  it('shows the five blocks with the framework words, in force 0 on every level', () => {
    render(<LawFrameworkView framework={framework} laws={[law('1', '素材')]} />)
    expect(document.querySelectorAll('[data-level]')).toHaveLength(5)
    expect(screen.getByText('根本法')).toBeTruthy()
    expect([...document.querySelectorAll('[data-in-force]')].map(e => e.textContent)).toEqual(Array(5).fill('In force 0'))
    expect(document.querySelector('[data-field="scope"]')?.textContent).toContain('适用范围')
    expect(screen.getByText('上位优先：下位法和上位法抵触的那一处不执行')).toBeTruthy()
    expect(screen.getByText('法只经 Owner 批准的修法票增、改、废')).toBeTruthy()
    expect(document.querySelector('[data-state="素材"]')?.textContent).toBe('素材 2')
  })
  it('says unknown where the platform sent nothing, never an empty or made-up value', () => {
    render(<LawFrameworkView framework={{ version: 'v1', levels: [{ level: 0, name: '道', meaning: '', required: ['text'], in_force: 0 }] }} laws={[]} />)
    expect(document.querySelector('[data-framework-block="between"]')?.textContent).toContain('unknown')
    expect(document.querySelector('[data-framework-block="fields"]')?.textContent).toContain('unknown')
    expect(document.querySelector('[data-state="现行"]')?.textContent).toBe('现行 unknown')
  })
})

describe('Command → Laws reads', () => {
  const body = { framework, laws: [law('83', '素材'), law('85', '素材'), law('11', '废止')] }
  const mount = () => render(<QueryClientProvider client={new QueryClient()}><LawRegister /></QueryClientProvider>)

  it('through the engine on STG only', () => {
    expect(engineLawsOn('stg')).toBe(true)
    expect(engineLawsOn('prod')).toBe(false)
    expect(engineLawsOn(undefined)).toBe(false)
  })
  it('on STG reads /engine/v1/agents?read=laws and draws the frame; material laws stay behind the switch', async () => {
    wire.env = 'stg'
    wire.read.mockResolvedValue(json({ version: 1, model: 'agents:laws', capturedAt: '2026-11-24T08:00:00Z', provenance: 'live', identity: { epoch: 'e1', role: 'viewer' }, evidence: [{ source: '/api/v1/agentrt/laws', outcome: 'read', receivedAt: '2026-11-24T08:00:00Z' }], data: body }))
    mount()
    await waitFor(() => expect(document.querySelectorAll('[data-level]')).toHaveLength(5))
    expect(wire.read.mock.calls.map(c => c[0])).toEqual(['/engine/v1/agents?read=laws'])
    expect(screen.getByText('framework v1 · 5 levels · 现行 0 · 素材 2 · 废止 0')).toBeTruthy()
    expect(screen.queryByText('law 83')).toBeNull()
    // 2026-11-24 立法线: the Owner read "85 laws" as 85 laws in the framework. The head counts by state; no "N laws".
    expect(document.body.textContent).not.toMatch(/\d+ laws/)
    fireEvent.click(screen.getByLabelText('Show material (素材): 2, not laws'))
    await waitFor(() => expect(screen.getByText('law 83')).toBeTruthy())
    expect(screen.queryByText('law 11')).toBeNull()
    // 2026-11-24 the Owner on the repealed: "我觉得没必要...删掉..对于法律来说都是毒药": no repealed row, no switch for them.
    expect(screen.queryByLabelText(/Show repealed/)).toBeNull()
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

// 2026-11-24 the Owner, step two: "我们一条条提炼，然后一条条放，最后一次性确认新法涵盖老的85条吐槽后，就可以把85条删掉".
// The platform counts the coverage (立法线 44065b1c: framework.coverage, absorbed_by, frame.absorbs); the page says it.
describe('step two: the material covered by laws in force', () => {
  it('says covered / material, and when all is covered that the material can be repealed', () => {
    expect(coverageLine({ coverage: { material: 69, covered: 0, uncovered: [] } })).toBe('material covered 0 / 69')
    expect(coverageLine({ coverage: { material: 69, covered: 69, uncovered: [] } })).toBe('material covered 69 / 69 · all covered: ready to repeal the material')
    expect(coverageLine({})).toBe('material covered: unknown')
  })
  it('lists the material no law absorbs yet first, and says which law absorbs the rest', () => {
    const rows = [{ ...law('3', '素材'), absorbed_by: ['90'] }, law('7', '素材'), law('5', '废止'), law('1', '素材')]
    expect(materialOrder(rows).map(l => l.law)).toEqual(['1', '7', '3', '5'])
    // 2026-11-25 the Owner: "以后法条统一叫LAW 开始，不用简写": the absorbing law is named by its code.
    const t = lawRegisterTable(materialOrder(rows), new Map([['90', 'LAW-5']]))
    const absorbed = (key: string) => t.rows.find(r => r.key === key)!.cells.at(-1)
    expect(absorbed('3')).toBe('LAW-5')
    // A material row keeps its register number, written "#n" so it is never read as a law.
    expect(t.rows.find(r => r.key === '7')!.cells[0]).toBe('#7')
    expect(absorbed('1')).toMatchObject({ text: 'none' })
    expect(absorbed('5')).toBe('—')
  })
  it('shows under its level what a law in force absorbs', () => {
    const inForce: RegisterLaw = { ...law('90', '现行', 2), code: 'LAW-5', frame: { level: 2, absorbs: ['3', '12'] } }
    render(<LawFrameworkView framework={{ ...framework, coverage: { material: 69, covered: 2, uncovered: [] } }} laws={[inForce]} />)
    expect(document.querySelector('[data-in-force-law="90"]')?.textContent).toContain('Absorbs material 3, 12')
    expect(document.querySelector('[data-in-force-law="90"]')?.textContent).toMatch(/^LAW-5: law 90/)
    expect(document.querySelector('[data-coverage]')?.textContent).toBe('material covered 2 / 69')
  })
})
