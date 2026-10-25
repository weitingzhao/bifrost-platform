import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { entryOf, resolveConsoleHash } from '../consoleRoutes'
import { DataPage } from '@/pages/shell/data/DataPage'
import { InfrastructurePage } from '@/pages/shell/infrastructure/InfrastructurePage'
import { scrollAfterShell } from '../useHashSection'

vi.mock('@/api/core', () => ({ fetchContext: async () => ({}) }))
vi.mock('@/pages/PluginGalleryPage', () => ({ PluginGalleryPage: () => <p>Plugin list</p> }))
vi.mock('@/pages/MarketDataManagePage', () => ({ MarketDataManagePage: () => <><p>Market coverage, blueprint and ingest</p><input aria-label="Market filter" /></> }))
vi.mock('@/pages/FlexQueryManagePage', () => ({ FlexQueryManagePage: () => <p>Flex management</p> }))
vi.mock('@/pages/ResearchEnginePage', () => ({ ResearchEnginePage: () => <p>Research forecasts, settlement and cost</p> }))
vi.mock('@/pages/ClusterPage', () => ({ ClusterPage: () => <p>Cluster controls</p> }))
vi.mock('@/pages/NetworkPage', () => ({ NetworkPage: () => <p>Network panels</p> }))
vi.mock('@/pages/shell/infrastructure/RuntimeMapSection', () => ({ RuntimeMapSection: () => <p>Runtime topology</p> }))
vi.mock('@/pages/shell/infrastructure/RuntimeHosts', () => ({ RuntimeHosts: () => <p>Runtime host readings</p> }))
vi.mock('@/pages/shell/infrastructure/MiniCards', () => ({ MiniCards: () => <p>Mini host controls</p> }))
vi.mock('@/pages/shell/data/BackupStatusPanel', () => ({ BackupStatusPanel: () => <p>Backup freshness</p> }))
afterEach(() => { cleanup(); vi.useRealTimers(); window.location.hash = '' })

describe('P3 domain destinations', () => {
  it.each([
    ['satellite-health?env=prod', '#services?section=applications&env=prod', 'services'],
    ['releases?section=versions&env=stg', '#services?section=versions&env=stg', 'services'],
    ['work?section=hosts&host=mbp-dev', '#infrastructure?section=hosts&host=mbp-dev', 'system'],
    ['data?section=backup&db=trade', '#infrastructure?section=backup&db=trade', 'system'],
    ['work?section=architecture&root=ARCH', '#crew?section=capability-evidence&root=ARCH', 'crew'],
    ['task-cc?thread=abc', '#work?section=activity&thread=abc', 'work'],
    ['research-engine?tab=cost&date=2026-10-24', '#data?section=research&tab=cost&date=2026-10-24', 'services'],
    ['flex-query-manage?account=test', '#data?section=flex&account=test', 'services'],
    ['network?device=one', '#infrastructure?section=network&device=one', 'system'],
    ['records?tab=invalid', '#records?tab=invalid', 'command'],
  ])('keeps parameters and domain for %s', (old, expected, domain) => {
    const resolved = resolveConsoleHash(old)
    expect(resolved.canonical).toBe(expected)
    expect(entryOf(resolved.location, expected.split('?')[1])).toBe(domain)
    expect(resolveConsoleHash(expected.slice(1)).canonical).toBe(expected)
  })
  it('retains every business component, with backup linked to its unique System detail', () => {
    render(<DataPage />)
    for (const name of ['Plugins', 'Market data', 'IB Flex', 'Research']) expect(screen.getByRole('region', { name })).toBeTruthy()
    expect(screen.queryByText('Backup freshness')).toBeNull()
    expect(screen.getByRole('link', { name: 'Storage and backup · System' }).getAttribute('href')).toBe('#infrastructure?section=backup')
  })
  it('focuses one business panel while preserving another panel filter', async () => {
    window.location.hash = '#data?section=market-data'
    render(<DataPage />)
    fireEvent.change(screen.getByRole('textbox', {name:'Market filter'}), {target:{value:'AAPL'}})
    expect(screen.queryByRole('region', {name:'Research'})).toBeNull()
    await act(async () => { window.location.hash = '#data?section=research'; window.dispatchEvent(new HashChangeEvent('hashchange')) })
    expect(screen.getByRole('region', {name:'Research'})).toBeTruthy()
    expect(screen.queryByRole('textbox', {name:'Market filter'})).toBeNull()
    await act(async () => { window.location.hash = '#data?section=market-data'; window.dispatchEvent(new HashChangeEvent('hashchange')) })
    expect((screen.getByRole('textbox', {name:'Market filter'}) as HTMLInputElement).value).toBe('AAPL')
  })
  it('retains network, cluster and mini controls alongside the moved hosts and backup', () => {
    const client = new QueryClient({defaultOptions: {queries: {retry:false}}})
    const view = render(<QueryClientProvider client={client}><InfrastructurePage /></QueryClientProvider>)
    for (const text of ['Cluster controls', 'Network panels', 'Runtime topology', 'Runtime host readings', 'Mini host controls', 'Backup freshness']) expect(screen.getByText(text)).toBeTruthy()
    view.unmount(); client.clear()
  })
  it('scrolls below wrapped sticky navigation, and cancels a pending destination', () => {
    vi.useFakeTimers()
    const chrome = document.createElement('div'); chrome.className = 'console-shell-chrome'
    chrome.getBoundingClientRect = () => ({ height: 240 } as DOMRect)
    const target = document.createElement('section'); target.scrollIntoView = vi.fn()
    document.body.append(chrome, target)
    const cancel = scrollAfterShell(() => target); cancel(); vi.runAllTimers()
    expect(target.scrollIntoView).not.toHaveBeenCalled()
    scrollAfterShell(() => target); vi.runAllTimers()
    expect(target.style.scrollMarginTop).toBe('256px')
    expect(target.scrollIntoView).toHaveBeenCalledWith({block:'start'})
    chrome.remove(); target.remove()
  })
})
