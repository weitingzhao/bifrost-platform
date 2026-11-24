// Audit conclusions with the audit line's sample (domains/2-lineage/audit/ui-audit-page-fields-and-sample-2026-11-24.md):
// STG's plan has no R6 nodes and PROD has no engine yet, so the page with data can only be looked at here. Labelled
// SAMPLE on the page itself; never a conclusion. The real page component, its cache seeded with the sample answers.
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { engineQueryKey } from '@/api/engine'
import type { LawStep, RegisterLaw } from '@/api/lawRegister'
import { AuditConclusions } from '@/pages/shell/owner/AuditConclusions'
import '../src/index.css'
import '../src/styles/opsAppearance.css'
document.documentElement.dataset.opsTheme = new URLSearchParams(window.location.search).get('theme') === 'dark' ? 'dark' : 'light'

const A = '/engine/v1/agents'
const step = (n: number, title: string, grade: string, attribution?: LawStep['attribution']): LawStep =>
  ({ plan_node_id: `R6:6.11-${n}`, title, grade, version: 'appr_648987184f28813a', ...(attribution ? { attribution } : {}) })
const laws: RegisterLaw[] = [{
  law: '83', version: 2, op: 'amend', text: '代码的结构跟着需求变（样例）', status: '已批', source: 'card', at: '2026-11-24T00:00:00Z', plan_item: 'R6:6.11',
  counts: { reached: 2, missed: 4, unmeasured: 1 },
  steps: [
    step(1, 's1：加新的：载荷和试验层交一页需求当天能用｜量法：提交时间到第一次 ci-* Succeeded。现在：领域级量得了', '达到'),
    step(4, 's4：升级：每层的升级条件写明、系统能判｜量法：check_repo_registry.py 判。现在：量不了：拦的检查 CI 线未排', '没达到', { category: '法', cause: '改成拦人属改裁判规则（法 33），批准算不算批示没写清', destination: '立法线' }),
    step(5, 's5：淘汰：系统能回答还有谁在用｜量法：get_package_users。现在：部分量得了：Secret 只能列名', '部分'),
    step(7, 's7：不弄坏别人：任何一次结构变动之后整套 CI 照过｜量法：每日 13:30Z 整套 CI。现在：缺每日 13:30Z 整套 CI 的结果', '没核'),
  ],
}]
const events: Record<string, unknown[]> = {
  'R6:6.11-1': [{ event_id: 195114, event_type: 'note', body: { text: '结论 s1：达到｜读数：仓库级第一例 bifrost-engine，登记 tier shared，见 #723、ci-engine-cbfcs（临时）' } }, { event_id: 195115, event_type: 'plan_step_judged', body: { note_event_id: 195114 } }],
  'R6:6.11-4': [{ event_id: 195123, event_type: 'note', body: { text: '结论 s4：没达到｜读数：check_repo_registry.py 只报不拦（infra 848cdc8e，系统存档）' } }, { event_id: 195124, event_type: 'plan_step_judged', body: { note_event_id: 195123 } }],
  'R6:6.11-5': [{ event_id: 195127, event_type: 'note', body: { text: '结论 s5：部分｜读数：包级 get_package_users 量得了，Secret 只能列名（系统存档）' } }, { event_id: 195128, event_type: 'plan_step_judged', body: { note_event_id: 195127 } }],
  'R6:6.11-7': [{ event_id: 195132, event_type: 'note', body: { text: '结论 s7：没核｜读数：该提交自己的整套 CI 通过 ci-platform-d27qq；evidence_id 126335（系统存档）' } }, { event_id: 195133, event_type: 'plan_step_judged', body: { note_event_id: 195132 } }],
}
const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false, refetchInterval: false, queryFn: () => new Promise(() => {}) } } })
client.setQueryData(engineQueryKey(A, { read: 'laws' }), laws)
for (const [node, ev] of Object.entries(events)) client.setQueryData(engineQueryKey(A, { read: 'plan-step', node }), ev)

if (import.meta.env.DEV) createRoot(document.getElementById('root')!).render(
  <QueryClientProvider client={client}>
    <main className="@container mx-auto flex max-w-6xl flex-col gap-3 bg-background p-4 text-foreground" style={{ minHeight: '100vh' }}>
      <p className="m-0 rounded border border-dashed border-[var(--border)] p-2 text-sm font-semibold">SAMPLE · the audit line’s sample of 2026-11-24 (law 83, four of its seven standards). Not a conclusion: the real page reads the UI engine.</p>
      <AuditConclusions />
    </main>
  </QueryClientProvider>,
)
