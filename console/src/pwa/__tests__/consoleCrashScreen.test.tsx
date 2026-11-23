import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ConsoleRootBoundary, RouteLoading } from '@/pwa/ConsoleCrashScreen'
import { crashReport, isChunkLoadError } from '@/pwa/consoleCrash'

// Origin (2026-11-23): the Console had no root error boundary, so one render error
// or one lazy chunk removed by a newer deploy left a white page with no evidence
// (2026-11-17 STG roster status; 2026-11-23 Owner's white preview, cause uncaptured).

function Throws({ error }: { error: Error }): never {
  throw error
}

describe('console root boundary', () => {
  afterEach(() => vi.restoreAllMocks())

  it('shows the crash screen with build, place and error instead of a white page', () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    render(
      <ConsoleRootBoundary>
        <Throws error={new TypeError("Cannot read properties of undefined (reading 'status')")} />
      </ConsoleRootBoundary>,
    )
    const alert = screen.getByRole('alert')
    expect(alert.getAttribute('data-console-crash')).toBe('render')
    expect(alert.textContent).toContain("TypeError: Cannot read properties of undefined (reading 'status')")
    expect(alert.textContent).toContain('build: ')
    expect(alert.textContent).toContain('viewport: ')
    expect(screen.getByRole('button', { name: 'Reload' })).toBeTruthy()
  })

  it('names a lazy chunk the server no longer has as a newer deploy', () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    render(
      <ConsoleRootBoundary>
        <Throws error={new TypeError('Failed to fetch dynamically imported module: https://ops.bifrost.lan/assets/CityScreen-old.js')} />
      </ConsoleRootBoundary>,
    )
    const alert = screen.getByRole('alert')
    expect(alert.getAttribute('data-console-crash')).toBe('chunk')
    expect(alert.textContent).toContain('newer Console was deployed')
  })

  it('renders children untouched when nothing throws', () => {
    render(<ConsoleRootBoundary><p>fine</p></ConsoleRootBoundary>)
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.getByText('fine')).toBeTruthy()
  })

  it('recognises the chunk errors of Chrome, Safari and Firefox and nothing else', () => {
    expect(isChunkLoadError(new TypeError('Failed to fetch dynamically imported module: x.js'))).toBe(true)
    expect(isChunkLoadError(new TypeError('Importing a module script failed.'))).toBe(true)
    expect(isChunkLoadError(new TypeError('error loading dynamically imported module: x.js'))).toBe(true)
    expect(isChunkLoadError(new Error('boom'))).toBe(false)
    expect(isChunkLoadError('Failed to fetch dynamically imported module')).toBe(false)
  })

  it('reports the hash it was on and a non-Error throw as text', () => {
    window.location.hash = '#city?look=city&at=work'
    const r = crashReport('plain string', null)
    expect(r.at).toBe('#city?look=city&at=work')
    expect(r.message).toBe('plain string')
    expect(r.chunk).toBe(false)
  })
})

describe('route loading', () => {
  it('is visible text, not an empty fallback', () => {
    render(<RouteLoading full place="Signals" />)
    expect(screen.getByRole('status').textContent).toBe('Loading Signals…')
  })
})

describe('index.html boot watchdog', () => {
  const html = fs.readFileSync(path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../index.html'), 'utf8')
  const source = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)].map(m => m[1]).find(s => s.includes('console-boot-report'))

  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  function boot() {
    vi.useFakeTimers()
    document.body.innerHTML = '<div id="root"></div>'
    new Function(source ?? '')()
  }

  it('says on the page that the Console did not start, without touching #root', () => {
    expect(source).toBeTruthy()
    boot()
    vi.advanceTimersByTime(15_000)
    const report = document.getElementById('console-boot-report')
    expect(report?.textContent).toContain('The Console did not start on this page')
    expect(report?.textContent).toContain('build: ')
    expect(document.getElementById('root')?.childElementCount).toBe(0)
  })

  it('stays silent when React draws, and removes itself if React draws late', () => {
    boot()
    vi.advanceTimersByTime(15_000)
    expect(document.getElementById('console-boot-report')).not.toBeNull()
    document.getElementById('root')!.appendChild(document.createElement('div'))
    vi.advanceTimersByTime(3_000)
    expect(document.getElementById('console-boot-report')).toBeNull()
  })

  it('does not fire before 12 s', () => {
    boot()
    vi.advanceTimersByTime(9_000)
    expect(document.getElementById('console-boot-report')).toBeNull()
  })
})
