// 2026-11-16 observation split: tenant environment controls must not leak into shared cards;
// extracting the hook must preserve viewer-bound fetch suppression and manual selection.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, renderHook, screen } from '@testing-library/react'
import { DomainCard } from '@/domains/tenant/observability/DomainCard'
import { GrafanaSoloEmbed } from '../components/GrafanaSoloEmbed'
import { buildObservabilityViewModel } from '../observabilityViewModel'
import { useObservabilitySnapshot } from '../useObservabilitySnapshot'

const queries = vi.hoisted(() => ({
  data: new Map<string, unknown>(),
  options: new Map<string, { enabled?: boolean; refetchInterval?: number }>(),
  refetches: new Map<string, ReturnType<typeof vi.fn>>(),
}))
vi.mock('@tanstack/react-query', () => ({ useQuery: (options: { queryKey: unknown[]; enabled?: boolean; refetchInterval?: number }) => {
  const key = JSON.stringify(options.queryKey)
  queries.options.set(key, options)
  if (!queries.refetches.has(key)) queries.refetches.set(key, vi.fn())
  return { data: queries.data.get(key), error: null, isLoading: false, isFetching: false, refetch: queries.refetches.get(key) }
} }))
const key = (...parts: string[]) => JSON.stringify(parts)
beforeEach(() => { queries.data.clear(); queries.options.clear(); queries.refetches.clear() })
afterEach(cleanup)

describe('observation domain boundary', () => {
  it('keeps tenant selection separate from selecting the card and absent on shared cards', () => {
    const vm = buildObservabilityViewModel({ selectedEnv: 'stg', selectedDomain: 'satellite', nowMs: 0 })
    const onSelect = vi.fn(), onTradeEnvChange = vi.fn()
    const { rerender } = render(<DomainCard domain={{ ...vm.domains.find(d => d.domain === 'satellite')!, envScope: 'env' }} selected onSelect={onSelect} tradeEnv="stg" onTradeEnvChange={onTradeEnvChange} namespace="bifrost-stg" />)
    fireEvent.click(screen.getByRole('button', { name: 'Prod' }))
    expect(onTradeEnvChange).toHaveBeenCalledExactlyOnceWith('prod')
    expect(onSelect).not.toHaveBeenCalled()
    rerender(<DomainCard domain={vm.domains.find(d => d.domain === 'rocket')!} selected onSelect={onSelect} tradeEnv="stg" onTradeEnvChange={onTradeEnvChange} />)
    expect(screen.queryByRole('group', { name: 'Satellite Trade environment' })).toBeNull()
  })
  it('preserves the Grafana iframe sandbox', () => {
    const { container } = render(<GrafanaSoloEmbed url="https://grafana.example/d-solo/one" title="Probe" height={180} />)
    const frame = container.querySelector('iframe')!
    expect(frame.getAttribute('sandbox')).toBe('allow-scripts allow-same-origin allow-popups allow-forms')
  })
  it('waits for viewer identity before scoped requests and ignores manual override in locked mode', () => {
    const { result, rerender } = renderHook(() => useObservabilitySnapshot({ followViewerOnly: true, includeEvidence: false }))
    expect(queries.options.get(key('telemetry', 'overview', result.current.namespace))?.enabled).toBe(false)
    expect(queries.options.get(key('satellite', 'bus-deep', result.current.tradeEnv))?.enabled).toBe(false)
    expect(queries.options.get(key('cluster', 'metrics'))?.enabled).toBe(false)
    queries.data.set(key('platform', 'self-health'), { viewer_env: 'prod' })
    rerender()
    expect(result.current.namespace).toBe('bifrost-prod')
    expect(queries.options.get(key('telemetry', 'overview', 'bifrost-prod'))?.enabled).toBe(true)
    act(() => result.current.setTradeEnv('dev'))
    expect(result.current.tradeEnv).toBe('prod')
  })
  it('does not overwrite a manual environment choice and preserves the explicit refetch set', () => {
    queries.data.set(key('platform', 'self-health'), { viewer_env: 'prod' })
    const { result, rerender } = renderHook(() => useObservabilitySnapshot())
    act(() => result.current.setTradeEnv('stg'))
    queries.data.set(key('platform', 'self-health'), { viewer_env: 'dev' })
    rerender()
    expect(result.current.namespace).toBe('bifrost-stg')
    act(() => result.current.refetchAll())
    const called = [...queries.refetches].filter(([, fn]) => fn.mock.calls.length > 0).map(([k]) => k)
    expect(called).toHaveLength(11)
    expect(called).toContain(key('satellite', 'bus-deep', 'stg'))
    expect(called).not.toContain(key('code-health', 'observability'))
    expect(called).not.toContain(key('network', 'sla', 'observability'))
    expect(queries.options.get(key('code-health', 'observability'))?.refetchInterval).toBe(300000)
  })
})
