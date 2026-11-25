// 2026-11-24 箭10: the audit-conclusions page (the Owner: 「审计基于立法的标准出结果，来监督行政」), built to the
// audit line's brief (domains/2-lineage/audit/ui-brief-audit-page-2026-11-24.md). The shapes are PROD's as read that
// day: LAW-11 (then law 83; R6:6.11, seven standards) and its step R6:6.11-4 graded 没达到 with note 195123.
import { describe, expect, it } from 'vitest'
import type { RegisterLaw } from '@/api/lawRegister'
import { attributionOf, auditDetail, auditTable, auditVerdict, auditWord, approvalsReading, ordered, periodOf, readingOf, startOf, readsApprovals, refsOf, resultOf, splitStandard, standardsOf, unmeasuredList, withEvents, type PlanEvent } from '../auditConclusionsModel'

const law = (n: string, grades: string[], counts = { reached: 0, missed: 0, unmeasured: 0 }): RegisterLaw => ({
  law: n, version: 2, op: 'amend', text: 'Law ' + n, status: '已批', source: 'card', at: '2026-11-24T00:00:00Z', plan_item: 'R6:6.' + n, counts,
  steps: grades.map((g, i) => ({ plan_node_id: `R6:6.${n}-${i + 1}`, title: `s${i + 1}：standard ${i + 1}｜量法：how ${i + 1}。现在：lacks reader ${i + 1}`, grade: g, version: 'appr_x' })),
})
const judged = (id: number, noteId: number): PlanEvent => ({ event_id: id, event_type: 'plan_step_judged', body: { grade: '没达到', note_event_id: noteId } })
const note = (id: number, text: string): PlanEvent => ({ event_id: id, event_type: 'note', body: { text } })
// Columns: 0 law · standard, 1 result, 2 numbers, 3 cause, 4 plan step.
const cell = (r: ReturnType<typeof standardsOf>[number], i: number) => auditTable([r]).rows[0].cells[i]

describe('audit conclusions', () => {
  it('folds the four grades into reached, missed and unmeasured (部分 is not reached)', () => {
    expect(['达到', '部分', '没达到', '没核', '', undefined].map(resultOf)).toEqual(['reached', 'missed', 'missed', 'unmeasured', 'unmeasured', 'unmeasured'])
  })

  it('splits a standard’s title into the standard, how it is measured and where it stands', () => {
    expect(splitStandard('s4：升级：条件写明｜量法：check_repo_registry.py 判。现在：量不了')).toEqual({ label: 's4', standard: '升级：条件写明', method: 'check_repo_registry.py 判。', now: '量不了' })
    // Control: a title not in that shape stays whole.
    expect(splitStandard('free text')).toEqual({ label: '', standard: 'free text', method: '', now: '' })
  })

  it('takes the three numbers from GET /laws, never recounting the steps, and says how many misses are unattributed', () => {
    // GET /laws says 2 / 4 / 1 while only three steps are listed here: the page shows GET /laws' numbers.
    const laws = [law('11', ['达到', '没达到', '没核'], { reached: 2, missed: 4, unmeasured: 1 })]
    const rows = standardsOf(laws)
    expect(auditVerdict(laws, rows)).toBe('2 reached · 4 missed · 1 unmeasured (1 laws, from GET /laws) · ? missed without a cause')
    expect(auditVerdict(laws, rows.map(r => withEvents(r, [])))).toContain('1 missed without a cause')
    expect(auditWord(laws)).toBe('warn')
    expect(auditWord(null)).toBe('unknown')
  })

  it('groups by law, misses first and unmeasured last, and folds an older plan item away', () => {
    const l = law('11', ['没核', '达到', '没达到'])
    l.steps!.push({ plan_node_id: 'R6:6.10-1', title: 's1：old', grade: '没达到' })
    const rows = ordered(standardsOf([law('12', ['达到']), l]))
    expect(rows.map(r => r.step)).toEqual(['R6:6.11-3', 'R6:6.11-2', 'R6:6.11-1', 'R6:6.12-1'])
  })

  it('reads the latest verdict’s note, marks its numbers kept or read once, and says when the note is outside the last 50 events', () => {
    const events = [note(100, '结论 s4：部分｜读数：old（临时）'), judged(101, 100), note(195123, '结论 s4：没达到｜读数：只报不拦（系统存档）'), judged(195124, 195123)]
    expect(readingOf(events)).toEqual(expect.objectContaining({ noteEventId: 195123, judgedEventId: 195124, found: true, source: 'archived' }))
    expect(readingOf([judged(9, 1)])).toEqual(expect.objectContaining({ found: false }))
    expect(readingOf([note(1, 'x')])).toBeNull()
    const [r] = standardsOf([law('11', ['没达到'])])
    expect(cell(withEvents(r, [note(7, '读数 x（临时）'), judged(8, 7)]), 2)).toEqual(expect.objectContaining({ text: 'read once', tone: 'unknown' }))
  })

  it('prefers the structured attribution from GET /laws, and reads a note (correction allowed) only without one', () => {
    const l = law('11', ['没达到'])
    l.steps![0].attribution = { category: '法', cause: '改成拦人属改裁判规则', destination: '立法线', version: 'appr_x' }
    const [structured] = standardsOf([l])
    expect(structured.attribution).toEqual(expect.objectContaining({ category: 'law', from: 'event', to: '立法线' }))
    // The structured one stays when the step's notes say otherwise.
    expect(withEvents(structured, [note(5, '归因 s1：类别 工具｜原因 x｜去向 y')]).attribution?.category).toBe('law')
    expect(attributionOf([note(6, '归因 s4（更正）：类别 Agent｜原因 没读｜去向 Crew｜标准版本 appr_ab12')], 's4')).toEqual(expect.objectContaining({ category: 'agent', from: 'note', version: 'appr_ab12' }))
    expect(attributionOf([note(6, '归因 s4：类别 Agent'), note(7, '归因 s4：作废')], 's4')).toBeNull()
  })

  it('keeps an unreadable attribution as written, its category unknown, never guessed', () => {
    const a = attributionOf([note(6, '归因 s1：大概是工具的问题')], 's1')
    expect(a).toEqual(expect.objectContaining({ category: 'unreadable', raw: '归因 s1：大概是工具的问题' }))
    const [r] = standardsOf([law('11', ['没达到'])])
    expect(cell(withEvents(r, [note(6, '归因 s1：大概是工具的问题')]), 3)).toEqual(expect.objectContaining({ text: 'unknown', tone: 'unknown' }))
    // Controls: not read is unknown with its reason; read and none is "not attributed"; a reached row has no cause.
    expect(cell(r, 3)).toEqual(expect.objectContaining({ text: 'unknown', title: expect.stringContaining('not read') }))
    expect(cell(withEvents(r, []), 3)).toEqual(expect.objectContaining({ text: 'not attributed' }))
    expect(cell(standardsOf([law('11', ['达到'])])[0], 3)).toBe('—')
    // The row opens by its first cell.
    expect(auditTable([r]).rows[0].open).toBe('R6:6.11-1')
  })

  it('lists what each unmeasured standard still lacks, and nothing for the measured ones', () => {
    expect(unmeasuredList(standardsOf([law('12', ['没核', '达到'])]))).toEqual([expect.objectContaining({ what: '#12 s1', lacks: 'lacks reader 1' })])
  })

  it('opens a row onto the conclusion and the cause apart, then the evidence one click away', () => {
    const [r] = standardsOf([law('11', ['没达到'])])
    const blocks = auditDetail(withEvents(r, [note(7, '结论 s1：没达到｜读数：见 #723 和 infra 848cdc8e，ci-engine-cbfcs，evidence_id 126335（系统存档）'), judged(8, 7)]))!
    const titles = blocks.map(b => (b as { head: { label: string }[] }).head[0].label)
    expect(titles).toEqual(['Conclusion', 'Cause', 'Evidence'])
    const evidence = (blocks[2] as { rows: { key: string; href?: string }[] }).rows
    expect(evidence[0].href).toContain('node=R6%3A6.11-1')
    expect(evidence.map(x => x.key)).toEqual(expect.arrayContaining(['judged', 'ref:approval #723', 'ref:commit 848cdc8e', 'ref:run ci-engine-cbfcs', 'ref:evidence_id 126335']))
  })

  // 2026-11-25 the Owner: laws are written LAW-k, never shortened (platform d9985668 sends code on every law in force).
  it('names a law by its code, and a row without one as #n so it is never taken for a law', () => {
    const coded = { ...law('97', ['没核']), code: 'LAW-12' } as RegisterLaw
    const [r] = standardsOf([coded])
    expect(auditTable([r]).rows[0].cells[0]).toEqual(expect.objectContaining({ text: expect.stringMatching(/^LAW-12 s1 · /) }))
    expect(unmeasuredList([r])[0].what).toBe('LAW-12 s1')
    const links = (auditDetail(r)![2] as { rows: { key: string; cells: unknown[]; href?: string }[] }).rows
    expect(links.find(x => x.key === 'law')).toEqual(expect.objectContaining({ cells: ['LAW-12 in the register'], href: expect.stringContaining('law=97') }))
    // Control: no code → #n.
    expect(auditTable(standardsOf([law('12', ['没核'])])).rows[0].cells[0]).toEqual(expect.objectContaining({ text: expect.stringMatching(/^#12 s1 · /) }))
  })

  it('finds the references a reading names and nothing else', () => {
    expect(refsOf('标准版本 appr_648987184f28813a，无别的')).toEqual([])
  })

  // 2026-11-24 the legislation line (#749): law 85 is archived and its standard moves into LAW-6 s1 ("每周为 0"). The
  // reading was tied to "law 85 s1" and would have hung on nothing; it now follows the standard that declares the read.
  it('gives the approvals readings to the standard that declares them, per week or per day as it says', () => {
    const readings = { days: [{ day: '2026-11-25', to_owner: { daily: [1] } }, { day: '2026-11-24', to_owner: { daily: [1, 2] } }, { day: '2026-11-23', to_owner: { daily: [1, 2, 3] } }] }
    const weekly = { standard: '推到 Owner 手机上的卡里，既不是修法票也不属于六类的，每周为 0', method: 'GET /api/v1/readings/approvals 的 days[].to_owner.daily', saidOn: '2026-11-24' }
    expect(readsApprovals(weekly)).toBe(true)
    expect(periodOf(weekly)).toBe('week')
    expect(approvalsReading(weekly, readings)).toMatch(/^3 cards in the last 2 day\(s\) · 2026-11-25: 1 · 2026-11-24: 2/)
    const daily = { ...weekly, standard: '日常卡每天为 0' }
    expect(periodOf(daily)).toBe('day')
    expect(approvalsReading(daily, readings)).toMatch(/^2026-11-25: 1 · 2026-11-24: 2 \(/)
    // Controls: another standard has no such reading; not read and no law date are said so.
    expect(readsApprovals({ standard: '整套 CI 照过', method: '每日 13:30Z 整套 CI' })).toBe(false)
    expect(approvalsReading(weekly, undefined)).toBe('not read')
    expect(approvalsReading({ ...weekly, saidOn: undefined }, readings)).toContain('not known')
  })

  // 2026-11-24 the legislation line: LAW-6 s1's method says 「从 2026-11-25 起计」 while the law was said on 11-24; the
  // page counted from said_on and would have added 11-24.
  it('counts from the start the method writes, and from the law’s date only when it writes none', () => {
    const readings = { days: [{ day: '2026-11-25', to_owner: { daily: [1] } }, { day: '2026-11-24', to_owner: { daily: [1, 2] } }] }
    const s91 = { standard: '既不是修法票也不属于六类的，每周为 0', method: 'GET /api/v1/readings/approvals 的 days[].to_owner.daily 条数（原第 85 条 s1 的读口），按周合计；与审批线行政台账同口径。从 2026-11-25 起计', saidOn: '2026-11-24' }
    expect(startOf(s91)).toEqual({ from: '2026-11-25', by: 'method' })
    expect(approvalsReading(s91, readings)).toMatch(/^1 cards in the last 1 day\(s\) · 2026-11-25: 1 \(/)
    // Control: without a written start, the law's date.
    expect(startOf({ method: 'GET /api/v1/readings/approvals', saidOn: '2026-11-24' })).toEqual({ from: '2026-11-24', by: 'law' })
  })
})
