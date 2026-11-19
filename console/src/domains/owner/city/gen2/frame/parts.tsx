import { createContext, useContext, useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

/**
 * The frame's parts (the Owner, 2026-11-19: like Bootstrap, the frame gives every panel the same
 * buttons, tabs, drop-downs, menus, dialogs, paging and scrolling, and fixes where each goes, so the
 * business that moves in uses them instead of making its own).
 *
 * Where things go in a panel:
 *   header   title and tabs on the left; the ⋯ menu, then ⤢ (to the middle), on the right
 *   toolbar  filters (Select) on the left, actions (Button) on the right
 *   body     scrolls inside itself (Scroll); never pushes the panel past its slot
 *   footer   paging (Pager) and where the numbers came from
 * Dialogs: the primary action at the bottom right, Cancel to its left; anything that destroys or
 * resets asks first (useConfirm). In a card (medium) a panel shows its compact form: tabs become a
 * Select, a pager shows fewer rows; in the middle (large) its full form.
 */

export type PanelSize = 'card' | 'middle' | 'drawer'

// ---- where dialogs and menus open: the frame's own root, so they take its colours ----

const Root = createContext<HTMLElement | null>(null)
export const KitRoot = Root.Provider

// ---- Button ----

export type ButtonKind = 'primary' | 'secondary' | 'ghost' | 'danger'

export function Button({ kind = 'secondary', onClick, children, disabled, label, type = 'button' }:
  { kind?: ButtonKind; onClick?: () => void; children: ReactNode; disabled?: boolean; label?: string; type?: 'button' | 'submit' }) {
  return <button type={type} className={`g2-btn k-${kind}`} onClick={onClick} disabled={disabled} aria-label={label}>{children}</button>
}

// ---- Tabs (a Select when the panel is a card: there is no room for a row of tabs) ----

export type Tab = { id: string; label: string }

export function Tabs({ tabs, value, onChange, compact }: { tabs: Tab[]; value: string; onChange: (id: string) => void; compact?: boolean }) {
  const refs = useRef<(HTMLButtonElement | null)[]>([])
  if (compact) return <Select value={value} onChange={onChange} options={tabs.map(t => ({ value: t.id, label: t.label }))} label="Tab" />
  const i = Math.max(0, tabs.findIndex(t => t.id === value))
  const go = (j: number) => { const k = (j + tabs.length) % tabs.length; onChange(tabs[k].id); refs.current[k]?.focus() }
  return <div className="k-tabs" role="tablist">
    {tabs.map((t, j) => (
      <button key={t.id} ref={el => { refs.current[j] = el }} role="tab" aria-selected={t.id === value} tabIndex={t.id === value ? 0 : -1}
        className={t.id === value ? 'on' : ''} onClick={() => onChange(t.id)}
        onKeyDown={e => { if (e.key === 'ArrowRight') go(i + 1); else if (e.key === 'ArrowLeft') go(i - 1) }}>{t.label}</button>
    ))}
  </div>
}

// ---- Select: the platform's own control (on a phone it opens the phone's own picker) ----

export function Select({ value, onChange, options, label }: { value: string; onChange: (v: string) => void; options: { value: string; label: string }[]; label: string }) {
  return <label className="k-select">
    <span className="k-sr">{label}</span>
    <select value={value} onChange={e => onChange(e.target.value)}>
      {options.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
    </select>
    <span aria-hidden className="k-caret">▾</span>
  </label>
}

// ---- Menu: the ⋯ button and what does not fit on the panel ----

export type MenuItem = { label: string; onClick: () => void; danger?: boolean }

export function Menu({ items, label = 'More' }: { items: MenuItem[]; label?: string }) {
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const away = (e: PointerEvent) => { if (!box.current?.contains(e.target as Node)) setOpen(false) }
    const esc = (e: KeyboardEvent) => { if (e.key === 'Escape') { e.stopPropagation(); setOpen(false) } }
    document.addEventListener('pointerdown', away)
    document.addEventListener('keydown', esc, true)
    return () => { document.removeEventListener('pointerdown', away); document.removeEventListener('keydown', esc, true) }
  }, [open])
  return <div className="k-menu" ref={box}>
    <button className="g2-icon" aria-haspopup="menu" aria-expanded={open} aria-label={label} onClick={() => setOpen(o => !o)}>⋯</button>
    {open && <ul role="menu">
      {items.map(it => <li key={it.label} role="none"><button role="menuitem" className={it.danger ? 'danger' : ''} onClick={() => { setOpen(false); it.onClick() }}>{it.label}</button></li>)}
    </ul>}
  </div>
}

// ---- Dialog: over the whole frame; primary at the bottom right; Esc or the backdrop cancels ----

export function Dialog({ title, children, actions, onClose }: { title: string; children: ReactNode; actions: ReactNode; onClose: () => void }) {
  const root = useContext(Root)
  const id = useId()
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    box.current?.querySelector<HTMLElement>('.k-primary, .k-danger, button')?.focus()
    const esc = (e: KeyboardEvent) => { if (e.key === 'Escape') { e.stopPropagation(); onClose() } }
    document.addEventListener('keydown', esc, true)
    return () => document.removeEventListener('keydown', esc, true)
  }, [onClose])
  const node = <div className="k-dialog-layer">
    <div className="k-backdrop" onClick={onClose} />
    <div className="k-dialog" role="dialog" aria-modal="true" aria-labelledby={id} ref={box}>
      <h3 id={id}>{title}</h3>
      <div className="k-dialog-body">{children}</div>
      <div className="k-dialog-actions">{actions}</div>
    </div>
  </div>
  return root ? createPortal(node, root) : node
}

/** Ask before anything that destroys or resets. Returns [ask, the dialog to render]. */
export function useConfirm(): [(o: { title: string; body: string; action: string; danger?: boolean; onConfirm: () => void }) => void, ReactNode] {
  const [ask, setAsk] = useState<{ title: string; body: string; action: string; danger?: boolean; onConfirm: () => void } | null>(null)
  const node = ask && <Dialog title={ask.title} onClose={() => setAsk(null)} actions={<>
    <Button kind="ghost" onClick={() => setAsk(null)}>Cancel</Button>
    <Button kind={ask.danger ? 'danger' : 'primary'} onClick={() => { const a = ask; setAsk(null); a.onConfirm() }}>{ask.action}</Button>
  </>}><p>{ask.body}</p></Dialog>
  return [setAsk, node]
}

// ---- Pager ----

/** The rows of one page, and what the pager says. Pages count from 1. */
export function pageOf<T>(rows: readonly T[], page: number, size: number): { rows: T[]; page: number; pages: number; from: number; to: number; total: number } {
  const pages = Math.max(1, Math.ceil(rows.length / size))
  const p = Math.min(Math.max(1, page), pages)
  const from = rows.length === 0 ? 0 : (p - 1) * size + 1
  const to = Math.min(rows.length, p * size)
  return { rows: rows.slice(from === 0 ? 0 : from - 1, to), page: p, pages, from, to, total: rows.length }
}

export function Pager({ page, pages, from, to, total, onPage }: { page: number; pages: number; from: number; to: number; total: number; onPage: (p: number) => void }) {
  return <nav className="k-pager" aria-label="Pages">
    <button className="g2-icon" onClick={() => onPage(page - 1)} disabled={page <= 1} aria-label="Previous page">‹</button>
    <span>{total === 0 ? 'None' : `${from}–${to} of ${total}`}</span>
    <button className="g2-icon" onClick={() => onPage(page + 1)} disabled={page >= pages} aria-label="Next page">›</button>
  </nav>
}

// ---- Layout pieces of a panel ----

/** Filters on the left, actions on the right. */
export function Toolbar({ left, right }: { left?: ReactNode; right?: ReactNode }) {
  return <div className="k-toolbar"><div className="k-left">{left}</div><div className="k-right">{right}</div></div>
}

/** A body that scrolls inside itself, with a fade where more is hidden. */
export function Scroll({ children, max }: { children: ReactNode; max?: number }) {
  return <div className="k-scroll" style={max ? { maxHeight: max } : undefined}>{children}</div>
}

/** Paging and where the numbers came from. */
export function Footer({ children }: { children: ReactNode }) {
  return <div className="k-footer">{children}</div>
}
