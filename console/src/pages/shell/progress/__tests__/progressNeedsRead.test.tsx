import { render, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import type { OwnerAsk } from '@/api/progressFields'
import { NowClock } from '@/pages/shell/progress/progressClock'
import { NeedsYouBlock } from '@/pages/shell/progress/NeedsYouBlock'
import {
  asksFromDocument,
  type ApprovalRead,
  type AskSource,
  type DocumentRead,
} from '@/pages/shell/progress/progressSummaryModel'

const NOW_SEC = Date.parse('2026-10-10T15:00:00Z') / 1000

function ask(partial: Partial<OwnerAsk> & Pick<OwnerAsk, 'unreadable'>): OwnerAsk {
  return partial
}

function gathered(owner: OwnerAsk[], standing?: OwnerAsk[]): AskSource[] {
  return asksFromDocument(owner, standing).asks
}

const CURRENT = gathered([
  ask({ unreadable: false, name: 'Sign', work: 'Open', now: true }),
  ask({ unreadable: false, name: 'Wait', work: 'Open', now: false }),
  ask({ unreadable: true, name: 'Broken' }),
])

type DocCase = {
  id: string
  documentRead: DocumentRead
  asks: AskSource[]
  current: number
}

const DOCUMENTS: DocCase[] = [
  { id: 'first load', documentRead: { kind: 'loading' }, asks: [], current: 0 },
  { id: 'failed, nothing cached', documentRead: { kind: 'failed', lastGoodAt: null }, asks: [], current: 0 },
  { id: 'failed, empty list cached', documentRead: { kind: 'failed', lastGoodAt: NOW_SEC }, asks: [], current: 0 },
  { id: 'failed, items cached', documentRead: { kind: 'failed', lastGoodAt: NOW_SEC }, asks: CURRENT, current: 0 },
  { id: 'ok, empty', documentRead: { kind: 'ok' }, asks: [], current: 0 },
  { id: 'ok, items', documentRead: { kind: 'ok' }, asks: CURRENT, current: 3 },
]

const APPROVALS: ApprovalRead[] = ['loading', 'unreadable', 'ready']

function mount(asks: AskSource[], read: ApprovalRead, documentRead: DocumentRead) {
  const wrap = (children: ReactNode) => <NowClock>{children}</NowClock>
  return render(
    wrap(
      <NeedsYouBlock
        asks={asks}
        items={undefined}
        read={read}
        listUnreadable={false}
        later={null}
        documentRead={documentRead}
      />,
    ),
  )
}

function hasGreen(root: HTMLElement): boolean {
  return [...root.querySelectorAll('*')].some(el => /emerald|bg-green|text-green/.test(String(el.className)))
}

describe('Needs you does not clear or count a bad read', () => {
  it('covers document read × approvals read', () => {
    for (const doc of DOCUMENTS) {
      for (const approvals of APPROVALS) {
        const view = mount(doc.asks, approvals, doc.documentRead)
        const block = view.getByRole('region', { name: 'Needs you' })
        const bothOk = doc.documentRead.kind === 'ok' && approvals === 'ready'
        const text = block.textContent ?? ''
        if (doc.documentRead.kind === 'loading') {
          expect(text, doc.id).toContain('Loading…')
          expect(block.querySelector('[data-needs-total]'), doc.id).toBeNull()
          expect(text, doc.id).not.toContain('Nothing needs you now')
          expect(hasGreen(block), doc.id).toBe(false)
          view.unmount()
          continue
        }
        const total = block.querySelector('[data-needs-total]')
        const n = Number(total?.getAttribute('data-needs-total'))
        const a = Number(block.querySelector('[data-can-now]')?.getAttribute('data-can-now'))
        const b = Number(block.querySelector('[data-not-yet]')?.getAttribute('data-not-yet'))
        const c = Number(block.querySelector('[data-needs-unknown]')?.getAttribute('data-needs-unknown') ?? '0')
        expect(text.includes('Nothing needs you now'), `${doc.id} / ${approvals}`).toBe(bothOk && doc.asks.length === 0)
        expect(n, `${doc.id} / ${approvals}`).toBe(doc.current)
        expect(n, `${doc.id} / ${approvals}`).toBe(a + b + c)
        expect(block.querySelectorAll('[data-ask]').length, doc.id).toBe(doc.current)
        if (!bothOk) expect(hasGreen(block), `${doc.id} / ${approvals}`).toBe(false)
        if (doc.documentRead.kind !== 'ok') {
          expect(text).toContain('Unknown — the progress document could not be read')
          expect(within(block).getByRole('link', { name: 'Requests waiting for you are listed there' }).getAttribute('href')).toBe('#needs-you')
          expect(block.querySelectorAll('[data-historical]').length).toBe(doc.asks.length)
          for (const row of block.querySelectorAll('[data-historical]')) {
            expect(row.textContent ?? '').toContain('as of')
            expect(row.querySelector('.bg-emerald-500')).toBeNull()
          }
        }
        if (bothOk && doc.id === 'ok, items') expect(hasGreen(block)).toBe(true)
        view.unmount()
      }
    }
  })

  it('says Not written unless the document itself says there is no deadline, and keeps blank actions in the agent area', () => {
    const bare = gathered([ask({ unreadable: false, name: 'Bare', now: true, actions: [{ text: '   ' }] })])
    const view = mount(bare, 'ready', { kind: 'ok' })
    const block = view.getByRole('region', { name: 'Needs you' })
    const row = block.querySelector('[data-ask="now"]') as HTMLElement
    expect(row.querySelector('[data-fact="Latest"]')?.textContent).toContain('Not written')
    expect(row.textContent ?? '').not.toContain('No deadline')
    const how = row.querySelector('[data-fact="How to do it"]') as HTMLElement
    expect(how.querySelector('[data-agent-note]')?.textContent).toContain('Not written')
    view.unmount()

    const said = gathered([ask({ unreadable: false, name: 'Said', now: true, deadline_text: 'No deadline' })])
    const saidView = mount(said, 'ready', { kind: 'ok' })
    const saidRow = saidView.getByRole('region', { name: 'Needs you' }).querySelector('[data-ask="now"]') as HTMLElement
    const words = [...saidRow.querySelectorAll('[data-fact="Latest"] [data-agent-text]')].find(node => node.textContent === 'No deadline')
    expect(words).toBeTruthy()
    saidView.unmount()
  })

  it('counts a repeated owner and standing row once, and keeps two blank rows', () => {
    const once = asksFromDocument(
      [ask({ unreadable: false, name: 'Sign', work: 'Open', now: true })],
      [ask({ unreadable: false, name: 'Sign', work: 'Open', now: false })],
    )
    expect(once.asks).toHaveLength(1)
    const sameId = 'appr_0123456789abcdef'
    const byId = asksFromDocument(
      [ask({ unreadable: false, name: 'A', now: true, approval: { id: sameId } })],
      [ask({ unreadable: false, name: 'B', now: false, approval: { id: sameId } })],
    )
    expect(byId.asks).toHaveLength(1)
    const blanks = asksFromDocument(
      [ask({ unreadable: false, now: true }), ask({ unreadable: false, now: true })],
      [ask({ unreadable: false, now: false })],
    )
    expect(blanks.asks).toHaveLength(3)
    const listed = asksFromDocument(
      [ask({ unreadable: false, name: 'Sign', work: 'Open', now: true })],
      [ask({ unreadable: false, name: 'Sign', work: 'Open', now: false })],
      undefined,
      [ask({ unreadable: false, name: 'Sign', work: 'Open', now: true }), ask({ unreadable: false, name: 'Sign', work: 'Open', now: false })],
    )
    expect(listed.asks).toHaveLength(2)
    const view = mount(once.asks, 'ready', { kind: 'ok' })
    expect(view.getByRole('region', { name: 'Needs you' }).querySelector('[data-needs-total]')?.getAttribute('data-needs-total')).toBe('1')
  })
})
