// 2026-11-24 PROD: Command → Audit conclusions said "engine unreachable: HTTP …" for the Owner, because the page read only
// /engine/v1/agents and PROD has no engine yet (箭10). The page now reads the engine where the switch says (STG) and the
// platform directly elsewhere, and a failed direct read says why.
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'

const env = vi.hoisted(() => ({ viewer_env: 'prod', fail: false }))
const direct = vi.hoisted(() => [] as string[])
const engine = vi.hoisted(() => [] as string[])
vi.mock('@/api/core', () => ({ fetchSelfHealth: async () => ({ viewer_env: env.viewer_env }) }))
vi.mock('@/api/approvals', () => ({
  viewerRead: async (_label: string, path: string) => {
    direct.push(path)
    if (env.fail) throw new Error('Law register: HTTP 403 viewer token required')
    const body = path === '/api/v1/agentrt/laws'
      ? { laws: [{ law: '83', version: 2, op: 'amend', text: 't', status: '已批', source: 'c', at: 'x', plan_item: 'R6:6.11', counts: { reached: 0, missed: 1, unmeasured: 0 }, steps: [{ plan_node_id: 'R6:6.11-4', title: 's4：升级｜量法：x。现在：y', grade: '没达到' }] }] }
      : { events: [] }
    return new Response(JSON.stringify(body))
  },
}))
vi.mock('@/api/engine', async importOriginal => ({
  ...(await importOriginal<typeof import('@/api/engine')>()),
  readEngine: async (path: string, opts: { query?: Record<string, string> }) => {
    engine.push(path + '?' + new URLSearchParams(opts.query).toString())
    return { reached: true, model: 'agents', capturedAt: 'x', identity: null, evidence: [], data: opts.query?.read === 'laws' ? { laws: [] } : { events: [] } }
  },
}))
import { AuditConclusions } from '../AuditConclusions'

afterEach(() => { cleanup(); direct.length = 0; engine.length = 0; env.fail = false })
const show = () => render(<QueryClientProvider client={new QueryClient()}><AuditConclusions /></QueryClientProvider>)

it('reads the platform directly on PROD and never the engine', async () => {
  env.viewer_env = 'prod'
  show()
  await waitFor(() => expect(direct).toContain('/api/v1/agentrt/plan/R6%3A6.11-4'))
  expect(direct).toContain('/api/v1/agentrt/laws')
  expect(engine).toEqual([])
  expect(screen.queryByText(/engine unreachable/)).toBeNull()
})

it('says why when the direct read fails, never a quiet fall back to the engine', async () => {
  env.viewer_env = 'prod'; env.fail = true
  show()
  expect(await screen.findByText(/could not be read: Law register: HTTP 403/)).toBeTruthy()
  expect(engine).toEqual([])
})

it('reads through the engine on STG (control)', async () => {
  env.viewer_env = 'stg'
  show()
  await waitFor(() => expect(engine.some(e => e.includes('read=laws'))).toBe(true))
  expect(direct).toEqual([])
})
