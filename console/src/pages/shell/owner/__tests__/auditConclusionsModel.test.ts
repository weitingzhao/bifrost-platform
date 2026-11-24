// 2026-11-24 箭10: the audit-conclusions page (the Owner: 「审计基于立法的标准出结果，来监督行政」). The shapes are PROD's
// as read that day: law 83 (R6:6.11, seven standards) and its step R6:6.11-4 graded 没达到 with note 195123; the
// attribution note format is the audit line's (ui-audit-page-fields-and-sample-2026-11-24).
import { describe, expect, it } from 'vitest'
import type { RegisterLaw } from '@/api/lawRegister'
import { attributionOf, auditDetail, auditTable, auditVerdict, auditWord, readingOf, resultOf, splitStandard, standardsOf, withEvents, type PlanEvent } from '../auditConclusionsModel'

const law = (n: string, grades: string[]): RegisterLaw => ({
  law: n, version: 1, op: 'amend', text: 'Law ' + n, status: '已批', source: 'card', at: '2026-11-24T00:00:00Z', plan_item: 'R6:6.' + n,
  counts: { reached: 0, missed: 0, unmeasured: 0 },
  steps: grades.map((g, i) => ({ plan_node_id: `R6:6.${n}-${i + 1}`, title: `s${i + 1}：standard ${i + 1}｜量法：how ${i + 1}。现在：now ${i + 1}`, grade: g, version: 'appr_x' })),
})

describe('audit conclusions', () => {
  it('folds the four grades into reached, missed and unmeasured (部分 is not reached)', () => {
    expect(['达到', '部分', '没达到', '没核', '', undefined].map(resultOf)).toEqual(['reached', 'missed', 'missed', 'unmeasured', 'unmeasured', 'unmeasured'])
  })

  it('splits a standard’s title into the standard, how it is measured and where it stands', () => {
    expect(splitStandard('s4：升级：条件写明｜量法：check_repo_registry.py 判。现在：量不了')).toEqual({ label: 's4', standard: '升级：条件写明', method: 'check_repo_registry.py 判。', now: '量不了' })
    // Control: a title not in that shape stays whole.
    expect(splitStandard('free text')).toEqual({ label: '', standard: 'free text', method: '', now: '' })
  })

  it('counts every standard of every law, and the laws that have none (never as reached)', () => {
    const laws = [law('11', ['达到', '没达到', '没核']), { ...law('9', []), plan_item: undefined, counts: undefined, steps: undefined }]
    expect(standardsOf(laws)).toHaveLength(3)
    expect(auditVerdict(laws)).toBe('3 standards in 1 laws · 1 reached · 1 missed · 1 unmeasured · 1 laws have no standards yet')
    expect(auditWord(laws)).toBe('warn')
    expect(auditWord(null)).toBe('unknown')
    expect(auditVerdict(null)).toContain('?')
  })

  const judged = (id: number, note: number): PlanEvent => ({ event_id: id, event_type: 'plan_step_judged', body: { step: 'R6:6.11-4', standard: 's4', grade: '没达到', note_event_id: note } })
  const note = (id: number, text: string): PlanEvent => ({ event_id: id, event_type: 'note', body: { text } })

  it('reads the latest verdict’s note as the evidence and says when it is outside the last 50 events', () => {
    const events = [note(195100, '结论 s4：部分｜读数：old（临时）'), judged(195101, 195100), note(195123, '结论 s4：没达到｜读数：只报不拦（infra origin/main 文件，系统存档）'), judged(195124, 195123)]
    expect(readingOf(events)).toEqual(expect.objectContaining({ noteEventId: 195123, found: true, source: 'archived' }))
    expect(readingOf([judged(195124, 190000)])).toEqual(expect.objectContaining({ found: false, noteEventId: 190000 }))
    // Control: a step never judged has no reading.
    expect(readingOf([note(1, 'x')])).toBeUndefined()
  })

  it('takes the latest 「归因 sN：」 note as the cause, and a 作废 withdraws it', () => {
    const a = note(192606, '归因 s4：类别 法｜原因：改成拦人属改裁判规则（法 33）｜去向：立法线')
    expect(attributionOf([a], 's4')).toEqual({ category: 'law', reason: '改成拦人属改裁判规则（法 33）', to: '立法线', noteEventId: 192606 })
    expect(attributionOf([a, note(192700, '归因 s4：作废，上一条归因不成立')], 's4')).toBeNull()
    // Control: another standard's attribution is not this one's.
    expect(attributionOf([a], 's3')).toBeNull()
  })

  it('says unknown for a missed standard’s cause, and why: not read, or not attributed yet', () => {
    const [missed] = standardsOf([law('11', ['没达到'])])
    const cause = (r: typeof missed) => auditTable([r]).rows[0].cells[3]
    expect(cause(missed)).toEqual(expect.objectContaining({ text: 'unknown', title: expect.stringContaining('could not be read') }))
    expect(cause(withEvents(missed, []))).toEqual(expect.objectContaining({ text: 'unknown', title: expect.stringContaining('not attributed') }))
    expect(cause(withEvents(missed, [note(5, '归因 s1：类别 工具｜原因：没有读口｜去向：Station')]))).toEqual(expect.objectContaining({ text: 'tool' }))
    // Control: a reached standard has no cause column.
    const [reached] = standardsOf([law('11', ['达到'])])
    expect(cause(reached)).toBe('—')
  })

  it('opens a row onto its evidence and the plan step, item and law', () => {
    const [r] = standardsOf([law('11', ['没达到'])])
    const blocks = auditDetail(withEvents(r, [note(7, '结论 s1：没达到｜读数：x（临时）'), judged(8, 7)]))!
    const evidence = (blocks[0] as { rows: { key: string; cells: unknown[] }[] }).rows
    expect(evidence.find(x => x.key === 'Reading')?.cells[1]).toBe('结论 s1：没达到｜读数：x（临时）')
    expect(evidence.find(x => x.key === 'Numbers')?.cells[1]).toBe('read once (ad hoc)')
    const links = (blocks[1] as { rows: { href?: string }[] }).rows.map(x => x.href)
    expect(links).toEqual([expect.stringContaining('node=R6%3A6.11-1'), expect.stringContaining('node=R6%3A6.11'), expect.stringContaining('law=11')])
  })
})
