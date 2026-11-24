// 2026-11-24 箭10: the register of laws gets its own entry on Command (GET /api/v1/agentrt/laws, Plan's read). Each
// law's standards read reached ok, missed warn, unmeasured unknown, never 0; a law no approved card has changed has no
// plan item and says so rather than counting as reached; the page leads with one verdict line.
import { describe, expect, it } from 'vitest'
import type { RegisterLaw } from '@/api/lawRegister'
import { gradeTone, lawDetail, lawHref, lawRegisterTable, lawRegisterVerdict, lawRegisterWord } from '@/pages/shell/owner/lawRegisterModel'

const law = (n: string, counts?: RegisterLaw['counts'], steps?: RegisterLaw['steps']): RegisterLaw => ({
  law: n, version: 1, op: 'transcribed', text: `law ${n}`, status: '已落', source: 'OWNER_LAW.md', at: '2026-11-24T00:00:00Z',
  ...(counts ? { counts, plan_item: `item-${n}` } : {}), ...(steps ? { steps } : {}),
})

describe('law register', () => {
  it('reads a step grade in the vocabulary', () => {
    expect(gradeTone('达到')).toBe('ok')
    expect(gradeTone('部分')).toBe('warn')
    expect(gradeTone('没达到')).toBe('warn')
    expect(gradeTone('没核')).toBe('unknown')
    expect(gradeTone(undefined)).toBe('unknown')
  })
  it('leads with one verdict line, and an unread register is "?"', () => {
    const laws = [law('1', { reached: 2, missed: 1, unmeasured: 3 }), law('2')]
    expect(lawRegisterVerdict(laws)).toBe('2 laws · 2 reached · 1 missed · 3 unmeasured · 1 without a plan item')
    expect(lawRegisterVerdict(null)).toBe('? laws · ? reached · ? missed · ? unmeasured')
    expect(lawRegisterWord(laws)).toBe('warn')
    expect(lawRegisterWord([law('1', { reached: 2, missed: 0, unmeasured: 0 })])).toBe('ok')
    expect(lawRegisterWord([law('1')])).toBe('unknown')
  })
  it('shows a law without a plan item as "—", not as 0 reached', () => {
    const t = lawRegisterTable([law('9')])
    expect(t.rows[0].cells[5]).toMatchObject({ text: '—' })
  })
  it('opens a law to its steps and plan item, each linking to the plan', () => {
    const blocks = lawDetail(law('5', { reached: 1, missed: 0, unmeasured: 0 }, [{ plan_node_id: 'n1', title: 'a step', grade: '达到' }]))!
    expect(blocks).toHaveLength(2)
    expect(JSON.stringify(blocks)).toContain('#work?node=n1')
    expect(lawDetail(law('6'))?.[0]).toMatchObject({ kind: 'note' })
    expect(lawHref('73')).toBe('#command?section=laws&law=73')
  })
})
