import { useSyncExternalStore } from 'react'
import { cameWith } from '@/lib/shell/sharedContext'

const subscribe = (l: () => void) => { window.addEventListener('hashchange', l); return () => window.removeEventListener('hashchange', l) }
const hash = () => window.location.hash

/**
 * The frame's second row when the address came with a focus this place does not show (from=, the shared context;
 * 箭10 2026-11-25: never dropped quietly). "Came with lane I4cec…: not shown here".
 */
export function CameWithNote({ className = '' }: { className?: string }) {
  const focus = cameWith(useSyncExternalStore(subscribe, hash, () => ''))
  if (!focus) return null
  const id = focus.id.length > 14 ? focus.id.slice(0, 12) + '…' : focus.id
  return <span className={className} data-came-with title={`Came with ${focus.kind} ${focus.id}; this place does not show it`}>{`Came with ${focus.kind} ${id}: not shown here`}</span>
}
