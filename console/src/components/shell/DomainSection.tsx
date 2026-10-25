import { useRef, type ReactNode } from 'react'
import { useHashSection } from '@/lib/shell/useHashSection'

/** A domain destination opens the same component and keeps its existing query and action state. */
export function DomainSection({ id, label, children, active = true }: { id: string; label: string; children: ReactNode; active?: boolean }) {
  const ref = useRef<HTMLElement>(null)
  useHashSection(id, ref)
  return <section ref={ref} id={id} aria-label={label} hidden={!active} className={active ? "flex min-w-0 scroll-mt-4 flex-col gap-3" : "hidden"}>{children}</section>
}
