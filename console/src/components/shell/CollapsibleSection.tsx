import { useEffect, useRef, useState, type ReactNode } from 'react'
import { hashNamesSection, useHashSection } from '@/lib/shell/useHashSection'

const KEY = (id: string) => `ops.section.${id}.closed`

const readClosed = (id: string): boolean => {
  try { return window.localStorage.getItem(KEY(id)) === '1' } catch { return false }
}
const writeClosed = (id: string, closed: boolean) => {
  try { if (closed) window.localStorage.setItem(KEY(id), '1'); else window.localStorage.removeItem(KEY(id)) } catch { /* storage blocked: only this visit remembers */ }
}

/**
 * A Command section the Owner can fold (Owner 2026-11-24: "这些section是不是可以收起来。默认展开"). Open by default;
 * folding it is remembered in this browser only. An address that names the section opens it again, so a link to it
 * never lands on a closed box.
 */
export function CollapsibleSection({ id, label, children }: { id: string; label: string; children: ReactNode }) {
  const ref = useRef<HTMLElement>(null)
  const [open, setOpen] = useState(() => !readClosed(id))
  useHashSection(id, ref)
  useEffect(() => {
    const sync = () => { if (hashNamesSection(window.location.hash, id)) setOpen(true) }
    sync()
    window.addEventListener('hashchange', sync)
    return () => window.removeEventListener('hashchange', sync)
  }, [id])
  const toggle = () => setOpen(o => { writeClosed(id, o); return !o })
  return (
    <section ref={ref} id={id} aria-label={label} data-collapsible={open ? 'open' : 'closed'} className="flex min-w-0 scroll-mt-4 flex-col gap-3">
      <button type="button" aria-expanded={open} aria-controls={`${id}-body`} onClick={toggle}
        className="flex min-h-11 w-full items-center gap-2 border-b border-[var(--table-rule)] text-left text-sm font-semibold">
        <span aria-hidden className="inline-block w-3 text-muted-foreground">{open ? '▾' : '▸'}</span>
        <span>{label}</span>
      </button>
      <div id={`${id}-body`} hidden={!open} className={open ? 'flex min-w-0 flex-col gap-3' : 'hidden'}>{children}</div>
    </section>
  )
}
