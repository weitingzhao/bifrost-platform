import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { clientStatus, connectionStatus } from '../busStatus'
import { BusSectionBoundary } from '../BusSectionBoundary'
import { nerveModel } from '../busNerve'
import type { BusClient } from '../useBusStream'

// Origin: 2026-11-17, STG. The roster answered "idle" for the dispatcher rows
// that had nothing to do yet (gate.RosterIdle); the Bus page's status table
// had no "idle", the lookup threw inside a render, and the whole Console went
// white on #bus. A status the page does not know is shown as itself, and one
// block that fails no longer takes the page with it.

afterEach(cleanup)

const client = (status: string): BusClient => ({ name: 'sub-card-created-to-owner', declared: true, receipts: 0, status })

describe('roster and connection statuses', () => {
  it('knows idle, and shows an unknown status as itself instead of throwing', () => {
    expect(clientStatus('idle')).toEqual({ label: 'idle', variant: 'neutral' })
    expect(clientStatus('lost').variant).toBe('danger')
    expect(clientStatus('paused-by-someone')).toEqual({ label: 'paused-by-someone', variant: 'neutral' })
    expect(connectionStatus('live').variant).toBe('success')
    expect(connectionStatus('something-new').label).toBe('something-new')
  })
  it('lets the nerve map take an idle or unknown reader', () => {
    const contract = { topics: [], subscriptions: [], clients: [] }
    const m = nerveModel(contract, [client('idle'), client('later-status')].map((c, i) => ({ ...c, name: `${c.name}-${i}` })))
    expect(m.readers.map(r => r.tone)).toEqual(['plain', 'plain'])
  })
})

describe('BusSectionBoundary', () => {
  it('shows a failing block\'s reason and keeps its neighbour drawn', () => {
    const quiet = vi.spyOn(console, 'error').mockImplementation(() => {})
    const Broken = () => { throw new Error("Cannot read properties of undefined (reading 'variant')") }
    render(
      <div>
        <BusSectionBoundary name="Contract"><Broken /></BusSectionBoundary>
        <BusSectionBoundary name="Generation"><p>generation drawn</p></BusSectionBoundary>
      </div>,
    )
    expect(screen.getByRole('alert').textContent).toContain('Contract could not be drawn')
    expect(screen.getByText('generation drawn')).toBeTruthy()
    quiet.mockRestore()
  })
})
