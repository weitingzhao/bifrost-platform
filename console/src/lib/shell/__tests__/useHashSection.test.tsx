import '@testing-library/jest-dom/vitest'
import { act, cleanup, render, waitFor } from '@testing-library/react'
import { useEffect, useRef, type ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { hashNamesSection, useHashSection } from '../useHashSection'

const log: string[] = []

/** The shell as ConsolePage does it: its effect scrolls to the top after the place's own effects. */
function Shell({ children }: { children: ReactNode }) {
  useEffect(() => { log.push('top') }, [])
  return <>{children}</>
}

function Section({ id }: { id: string }) {
  const ref = useRef<HTMLElement>(null)
  useHashSection(id, ref)
  return <section id={id} ref={ref} />
}

afterEach(() => { cleanup(); log.length = 0; window.history.replaceState(null, '', '/') })

describe('useHashSection', () => {
  it('reads section= from the hash, and nothing else', () => {
    expect(hashNamesSection('#you?section=waiting', 'waiting')).toBe(true)
    expect(hashNamesSection('#you?goal=waiting', 'waiting')).toBe(false)
    expect(hashNamesSection('#you', 'waiting')).toBe(false)
  })

  it('scrolls the named section into view after the shell has scrolled to the top, and on a later hash change', async () => {
    Element.prototype.scrollIntoView = vi.fn(function (this: Element) { log.push(`into:${this.id}`) })
    window.history.replaceState(null, '', '#you?section=waiting')
    render(<Shell><Section id="waiting" /><Section id="other" /></Shell>)
    await waitFor(() => expect(log.at(-1)).toBe('into:waiting'))
    expect(log).toEqual(['top', 'into:waiting'])
    await act(async () => { window.location.hash = '#you?section=other'; window.dispatchEvent(new Event('hashchange')) })
    await waitFor(() => expect(log.at(-1)).toBe('into:other'))
    expect(log.filter(entry => entry === 'into:waiting')).toHaveLength(1)
  })
})
