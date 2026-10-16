import type { ReactNode } from 'react'
import { TONE_COLOR, type Tone } from './ownerModel'

export function Dot({ tone, size = 'size-2.5' }: { tone: Tone; size?: string }) {
  return <span aria-hidden className={`inline-block ${size} shrink-0 rounded-full`} style={{ backgroundColor: TONE_COLOR[tone] }} />
}

/** A block: a card with a heading and, on the right, a link to its detail page. */
export function Card({ title, more, children, label, accent = false }: {
  title: ReactNode; more?: { href: string; text: string }; children: ReactNode; label: string; accent?: boolean
}) {
  return <section aria-label={label} className={`flex min-w-0 flex-col gap-3 rounded-[var(--card-radius)] border bg-[var(--card-fill)] p-4 ${accent ? 'border-[var(--color-warning)]' : 'border-[var(--card-border)]'}`}>
    <header className="flex flex-wrap items-center justify-between gap-2">
      <h3 className="m-0 text-base font-semibold">{title}</h3>
      {more && <a href={more.href} className="-my-2 inline-flex min-h-11 items-center text-sm underline-offset-2 hover:underline">{more.text} ›</a>}
    </header>
    {children}
  </section>
}

/** Kept by hand by the coordinator thread: a mark, with where it came from on hover. */
export function HandMark({ title }: { title: string }) {
  return <span title={title} className="text-xs text-[var(--color-warning)]">✎ by hand</span>
}

export function Bar({ percent, tone }: { percent: number; tone: Tone }) {
  const width = Math.max(0, Math.min(100, Math.round(percent)))
  return <div className="h-2 overflow-hidden rounded-full bg-[var(--control-fill)]" role="presentation">
    <div className="h-2 rounded-full" style={{ width: `${width}%`, backgroundColor: TONE_COLOR[tone] }} />
  </div>
}

export function Stat({ value, label, tone }: { value: ReactNode; label: string; tone?: Tone }) {
  return <div className="min-w-0 rounded-[var(--control-radius)] bg-[var(--control-fill)] p-2.5">
    <div className="text-2xl font-bold leading-tight" style={tone ? { color: TONE_COLOR[tone] } : undefined}>{value}</div>
    <div className="text-xs text-muted-foreground">{label}</div>
  </div>
}

/** A tappable link styled as a button: at least 44px high for a finger. */
export function ActionLink({ href, children, primary = false }: { href: string; children: ReactNode; primary?: boolean }) {
  return <a href={href} className={`inline-flex min-h-11 shrink-0 items-center rounded-[var(--control-radius)] px-3 text-sm font-semibold no-underline ${primary ? 'bg-foreground text-background' : 'border border-[var(--card-border)] text-foreground'}`}>{children}</a>
}
