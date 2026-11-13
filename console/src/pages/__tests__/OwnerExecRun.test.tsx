import '@testing-library/jest-dom/vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { OwnerExecRun, ownerExecCandidate } from '@/pages/OwnerExecRun'

vi.mock('@/hooks/usePlatformAuth', () => ({ usePlatformAuth: () => ({ token: 'fixture-only', caps: { role: 'admin' } }) }))
vi.mock('@/api/serverContact', () => ({ serverFetch: (input: RequestInfo | URL, init?: RequestInit) => fetch(input, init) }))

const COMMAND = 'echo owner-exec-ok'
// sha256 of COMMAND, from `printf %s 'echo owner-exec-ok' | shasum -a 256`.
const COMMAND_SHA = '63f65e4977e44cdc327eb9416979f6a46cbf40235273327f0c35e7ab7d540ce9'

function card(over: Record<string, unknown> = {}) {
  return buildPlatformApproval({
    id: 'fixture-run', action: 'owner_run_command', tier: 'D', status: 'pending', runner: 'owner',
    params: { command: COMMAND }, params_hash: 'fixture-hash', approval_line: 'fixture-line', ...over,
  })
}

type Call = { url: string; init?: RequestInit }
let calls: Call[]
let health: unknown
let runResponse: () => Response

beforeEach(() => {
  calls = []
  health = { ok: true }
  runResponse = () => new Response(JSON.stringify({ status: 'running', id: 'fixture-run' }), { status: 202 })
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    calls.push({ url, init })
    if (url === '/api/owner-exec/health') return new Response(JSON.stringify(health), { status: health == null ? 404 : 200 })
    return runResponse()
  }))
})
afterEach(() => { cleanup(); vi.unstubAllGlobals() })

const runCalls = () => calls.filter(c => c.url === '/api/owner-exec/run')
const codeField = () => screen.findByLabelText('Code from your authenticator app')

describe('approve and run with a one-time code', () => {
  it('is offered only for an owner_run_command the runner can take', () => {
    expect(ownerExecCandidate(card())).toBe(true)
    expect(ownerExecCandidate(card({ status: 'approved' }))).toBe(true)
    expect(ownerExecCandidate(card({ status: 'executed' }))).toBe(false)
    expect(ownerExecCandidate(card({ runner: 'host' }))).toBe(false)
    expect(ownerExecCandidate(card({ action: 'gitops_sync_app' }))).toBe(false)
    expect(ownerExecCandidate(card({ drill: true }))).toBe(false)
  })

  it('stays hidden when no runner answers', async () => {
    health = null
    render(<OwnerExecRun item={card()} blocked={null} onStarted={() => {}} />)
    await waitFor(() => expect(calls.length).toBe(1))
    expect(screen.queryByLabelText('Code from your authenticator app')).not.toBeInTheDocument()
  })

  it('keeps the button off until six digits are typed, and shows the command', async () => {
    render(<OwnerExecRun item={card()} blocked={null} onStarted={() => {}} />)
    const field = await codeField()
    expect(screen.getByText(COMMAND)).toBeInTheDocument()
    const button = screen.getByRole('button', { name: 'Approve and run' })
    expect(button).toBeDisabled()
    fireEvent.change(field, { target: { value: '12a34' } })
    expect(field).toHaveValue('1234')
    expect(button).toBeDisabled()
    fireEvent.change(field, { target: { value: '123456' } })
    expect(button).toBeEnabled()
  })

  it('sends the code in the body with the fingerprint and the approve body, never in the URL', async () => {
    const started = vi.fn()
    render(<OwnerExecRun item={card()} blocked={null} onStarted={started} />)
    fireEvent.change(await codeField(), { target: { value: '123456' } })
    fireEvent.click(screen.getByRole('button', { name: 'Approve and run' }))
    await waitFor(() => expect(started).toHaveBeenCalledTimes(1))
    const [call] = runCalls()
    expect(call.url).toBe('/api/owner-exec/run')
    expect(calls.every(c => !c.url.includes('123456') && !c.url.includes('fixture-only'))).toBe(true)
    expect(new Headers(call.init?.headers).get('Authorization')).toBe('Bearer fixture-only')
    const body = JSON.parse(String(call.init?.body))
    expect(body).toMatchObject({
      id: 'fixture-run', code: '123456',
      approve: { channel: 'console', approval_line: 'fixture-line', params_hash: 'fixture-hash' },
    })
    expect(body.command_sha256).toBe(COMMAND_SHA)
    expect(screen.getByRole('status')).toHaveTextContent('Started')
    expect(await codeField()).toHaveValue('')
  })

  it('runs an approved card without an approve body', async () => {
    render(<OwnerExecRun item={card({ status: 'approved' })} blocked={null} onStarted={() => {}} />)
    fireEvent.change(await codeField(), { target: { value: '654321' } })
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await waitFor(() => expect(runCalls().length).toBe(1))
    expect(JSON.parse(String(runCalls()[0].init?.body)).approve).toBeUndefined()
  })

  it('shows a refusal, clears the code and lets the next code be tried', async () => {
    runResponse = () => new Response(JSON.stringify({ error: 'wrong code' }), { status: 403 })
    const started = vi.fn()
    render(<OwnerExecRun item={card()} blocked={null} onStarted={started} />)
    const field = await codeField()
    fireEvent.change(field, { target: { value: '000000' } })
    fireEvent.click(screen.getByRole('button', { name: 'Approve and run' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('wrong code')
    expect(started).not.toHaveBeenCalled()
    expect(field).toHaveValue('')
    expect(field).toBeEnabled()
  })

  it.each([
    ['HTML', () => new Response('<html>fallback</html>', { status: 200 })],
    ['empty', () => new Response(null, { status: 204 })],
    ['missing fields', () => new Response('{}', { status: 202 })],
    ['wrong id', () => new Response(JSON.stringify({ status: 'running', id: 'another-card' }), { status: 202 })],
    ['wrong state', () => new Response(JSON.stringify({ status: 'failed', id: 'fixture-run' }), { status: 202 })],
    ['wrong HTTP status', () => new Response(JSON.stringify({ status: 'running', id: 'fixture-run' }), { status: 200 })],
  ])('does not claim Started for an invalid acknowledgement: %s', async (_name, response) => {
    runResponse = response
    const started = vi.fn()
    render(<OwnerExecRun item={card()} blocked={null} onStarted={started} />)
    fireEvent.change(await codeField(), { target: { value: '123456' } })
    fireEvent.click(screen.getByRole('button', { name: 'Approve and run' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('could not be confirmed')
    expect(started).not.toHaveBeenCalled()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(runCalls()).toHaveLength(1)
  })

  it('does not send while decisions are blocked', async () => {
    render(<OwnerExecRun item={card()} blocked="Sign in as admin to decide." onStarted={() => {}} />)
    fireEvent.change(await codeField(), { target: { value: '123456' } })
    expect(screen.getByRole('button', { name: 'Approve and run' })).toBeDisabled()
    expect(screen.getByText('Sign in as admin to decide.')).toBeInTheDocument()
    expect(runCalls().length).toBe(0)
  })
})
