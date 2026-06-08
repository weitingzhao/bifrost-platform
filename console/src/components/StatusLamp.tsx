import type { AuthStatus, Reachability } from '@/api/types'

const reachClass: Record<Reachability, string> = {
  ok: 'lamp-ok',
  degraded: 'lamp-degraded',
  fail: 'lamp-fail',
  unknown: 'lamp-unknown',
}

const authClass: Record<AuthStatus, string> = {
  ok: 'lamp-ok',
  missing: 'lamp-degraded',
  invalid: 'lamp-fail',
  skipped: 'lamp-unknown',
  blocked: 'lamp-fail',
}

export function StatusLamp({
  value,
  kind = 'reach',
}: {
  value: Reachability | AuthStatus
  kind?: 'reach' | 'auth'
}) {
  const cls = kind === 'auth' ? authClass[value as AuthStatus] : reachClass[value as Reachability]
  return (
    <span className={cls} aria-hidden>
      ●
    </span>
  )
}
