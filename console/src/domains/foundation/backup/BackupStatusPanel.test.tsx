import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { BackupStatusPanel } from './BackupStatusPanel'
import { fetchClusterPostgresBackupStatus } from './api'

const transport = vi.hoisted(() => vi.fn())
vi.mock('@/api/serverContact', () => ({ serverFetch: transport }))
let client: QueryClient | undefined
function show() {
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  render(<QueryClientProvider client={client}><BackupStatusPanel /></QueryClientProvider>)
}
afterEach(() => { cleanup(); client?.clear(); transport.mockReset() })

describe('Foundation backup read-only boundary', () => {
  it('keeps the original GET endpoint and propagates an HTTP failure', async () => {
    transport.mockResolvedValue(new Response('', { status: 503 }))
    await expect(fetchClusterPostgresBackupStatus()).rejects.toThrow('cluster postgres backup-status: HTTP 503')
    expect(transport).toHaveBeenCalledExactlyOnceWith('/api/v1/cluster/postgres/backup-status')
  })
  it('keeps loading distinct from a read result', () => {
    transport.mockImplementation(() => new Promise(() => {}))
    show()
    expect(screen.getByText('Loading…')).toBeTruthy()
  })
  it('displays the recorded status and shares the existing query key', async () => {
    const data = { fresh: true, signal: 'ok', detail: 'backup observed', last_backup_name: 'backup-1', last_backup_phase: 'completed', age_hours: 1.5, max_age_hours: 26, backup_count: 1, generated_at: '2026-11-16T15:00:00Z' }
    transport.mockResolvedValue(new Response(JSON.stringify(data)))
    show()
    expect(await screen.findByText('backup observed')).toBeTruthy()
    expect(screen.getByText(/backup-1 · completed · 1.5h/)).toBeTruthy()
    expect(client?.getQueryData(['cluster', 'postgres', 'backup-status'])).toEqual(data)
    expect(transport).toHaveBeenCalledExactlyOnceWith('/api/v1/cluster/postgres/backup-status')
  })
  it('displays a read failure without replacing it with a healthy reading', async () => {
    transport.mockRejectedValue(new Error('backup offline'))
    show()
    expect(await screen.findByText('backup offline')).toBeTruthy()
    expect(screen.queryByText('ok')).toBeNull()
  })
})
